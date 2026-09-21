// Тема «как в системе». Сервер без скрипта показывает оформление таким,
// каким оно нарисовано; здесь, до отрисовки, уточняем по системной
// настройке — потому и в <head>, а не в конце страницы: иначе мигало бы.
(function () {
  var root = document.documentElement;
  if (root.getAttribute('data-theme-mode') !== 'system' || !window.matchMedia) return;
  var mq = window.matchMedia('(prefers-color-scheme: dark)');
  function apply() { root.setAttribute('data-theme', mq.matches ? 'dark' : 'light'); }
  apply();
  if (mq.addEventListener) mq.addEventListener('change', apply);
})();
