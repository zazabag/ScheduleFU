#!/usr/bin/env bash
# Включение раздела «Пары» на сервере: ffmpeg, sherpa-onnx и модель GigaAM v3.
#
# Отдельный скрипт, а не часть install.sh, по двум причинам: качать четверть
# гигабайта на каждой выкатке незачем, и раздел может быть не нужен вовсе.
# Запускается один раз, повторный запуск ничего не ломает.
#
#   ssh root@сервер 'bash /opt/schedulefu/deploy/install-notes.sh'
#
# Ключ модели конспекта кладётся отдельно и только в окружение:
#   echo 'SCHEDULEFU_NOTES_LLM_API_KEY=...' >> /opt/schedulefu/env
set -euo pipefail

APP_DIR="${APP_DIR:-/opt/schedulefu}"
APP_USER="${APP_USER:-schedulefu}"
SHERPA_VERSION="${SHERPA_VERSION:-v1.13.8}"
# Модель GigaAM v3 (MIT) в виде, пригодном для sherpa-onnx.
MODEL_REPO="${MODEL_REPO:-csukuangfj/sherpa-onnx-nemo-transducer-giga-am-v3-russian-2025-12-16}"
MODEL_DIR="$APP_DIR/models/gigaam-v3"
SHERPA_DIR="$APP_DIR/sherpa"

echo "==> ffmpeg"
if ! command -v ffmpeg >/dev/null; then
  apt-get update -qq
  DEBIAN_FRONTEND=noninteractive apt-get install -y -qq ffmpeg
fi
ffmpeg -version | head -1

echo "==> sherpa-onnx $SHERPA_VERSION"
# Сборка shared, а не static: 25 МБ против 400. Библиотеки рядом с
# бинарником, путь к ним прописан в обёртке ниже.
if [ ! -x "$SHERPA_DIR/bin/sherpa-onnx-vad-with-offline-asr" ]; then
  TMP="$(mktemp -d)"
  NAME="sherpa-onnx-$SHERPA_VERSION-linux-x64-shared-no-tts"
  curl -fsSL -o "$TMP/sherpa.tar.bz2" \
    "https://github.com/k2-fsa/sherpa-onnx/releases/download/$SHERPA_VERSION/$NAME.tar.bz2"
  tar -xf "$TMP/sherpa.tar.bz2" -C "$TMP"
  rm -rf "$SHERPA_DIR"
  mkdir -p "$SHERPA_DIR"
  cp -r "$TMP/$NAME/bin" "$TMP/$NAME/lib" "$SHERPA_DIR/"
  rm -rf "$TMP"
fi

# Обёртка: systemd-юнит зовёт один исполняемый файл и ничего не знает про
# LD_LIBRARY_PATH, а конфиг ссылается на постоянное имя.
cat > "$APP_DIR/bin-sherpa-asr" <<WRAP
#!/bin/sh
# Создан deploy/server/install-notes.sh; правки затрутся.
exec env LD_LIBRARY_PATH="$SHERPA_DIR/lib:\$LD_LIBRARY_PATH" \\
  "$SHERPA_DIR/bin/sherpa-onnx-vad-with-offline-asr" "\$@"
WRAP
chmod +x "$APP_DIR/bin-sherpa-asr"

echo "==> Модель GigaAM v3 (~230 МБ)"
mkdir -p "$MODEL_DIR"
# Квантован только энкодер: он и весит почти всё. Декодер и джойнер — как
# есть, у них по паре мегабайт.
for f in encoder.int8.onnx decoder.onnx joiner.onnx tokens.txt; do
  if [ ! -s "$MODEL_DIR/$f" ]; then
    echo "    $f"
    curl -fsSL -o "$MODEL_DIR/$f" "https://huggingface.co/$MODEL_REPO/resolve/main/$f"
  fi
done
if [ ! -s "$MODEL_DIR/silero_vad.onnx" ]; then
  echo "    silero_vad.onnx"
  curl -fsSL -o "$MODEL_DIR/silero_vad.onnx" \
    "https://github.com/k2-fsa/sherpa-onnx/releases/download/asr-models/silero_vad.onnx"
fi

install -d -o $APP_USER -g $APP_USER -m 700 "$APP_DIR/audio"
chown -R $APP_USER:$APP_USER "$APP_DIR/models" "$SHERPA_DIR" "$APP_DIR/bin-sherpa-asr"

echo "==> Проверка распознавания на образце"
TESTWAV="$(mktemp -d)/example.wav"
curl -fsSL -o "$TESTWAV" "https://huggingface.co/$MODEL_REPO/resolve/main/test_wavs/example.wav"
"$APP_DIR/bin-sherpa-asr" \
  --silero-vad-model="$MODEL_DIR/silero_vad.onnx" \
  --encoder="$MODEL_DIR/encoder.int8.onnx" \
  --decoder="$MODEL_DIR/decoder.onnx" \
  --joiner="$MODEL_DIR/joiner.onnx" \
  --tokens="$MODEL_DIR/tokens.txt" \
  --model-type=nemo_transducer --num-threads="$(nproc)" "$TESTWAV" 2>/dev/null
rm -rf "$(dirname "$TESTWAV")"

echo
echo "Осталось руками:"
echo "  1) ключ модели конспекта:"
echo "     echo 'SCHEDULEFU_NOTES_LLM_API_KEY=...' >> $APP_DIR/env"
echo "  2) в $APP_DIR/config.yaml — notes.enabled: true,"
echo "     notes.asr.command: $APP_DIR/bin-sherpa-asr,"
echo "     notes.asr.threads: $(nproc)"
echo "  3) systemctl restart schedulefu-serve && systemctl enable --now schedulefu-notes"
