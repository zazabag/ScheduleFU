// Команда vapid печатает новую пару ключей для push-уведомлений.
//
// Ключи создаются один раз и живут вместе с сервисом: при их смене все
// существующие подписки перестают работать, и людям придётся подписываться
// заново. Поэтому приватный ключ хранится вне репозитория — в переменных
// окружения на сервере.
package main

import (
	"fmt"
	"os"

	"github.com/zazabag/schedulefu/internal/push"
)

func main() {
	keys, err := push.GenerateKeys()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Printf("VAPID_PUBLIC_KEY=%s\nVAPID_PRIVATE_KEY=%s\n", keys.Public, keys.Private)
	fmt.Fprintln(os.Stderr, "\nПриватный ключ в репозиторий не коммитить.")
	fmt.Fprintln(os.Stderr, "Публичный ключ попадёт в браузеры — это нормально, он для того и нужен.")
}
