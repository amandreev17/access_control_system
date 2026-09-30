import React from 'react';
import ReactDOM from 'react-dom';
import './index.css';
import App from './App';

ReactDOM.render(
  <React.StrictMode>
    <App />
  </React.StrictMode>,
  document.getElementById('root')
);

// Register service worker for PWA
if ('serviceWorker' in navigator) {
  // Было ли приложение под управлением SW до этой загрузки (значит это обновление, а не первый запуск)
  const hadController = !!navigator.serviceWorker.controller;

  window.addEventListener('load', () => {
    navigator.serviceWorker
      .register('/service-worker.js', { updateViaCache: 'none' })
      .then((registration) => {
        console.log('SW registered:', registration.scope);
        // Принудительно проверяем обновления при каждом запуске,
        // иначе телефон может месяцами работать на старой сборке из кэша
        registration.update().catch(() => {});
      })
      .catch((error) => {
        console.log('SW registration failed:', error);
      });

    // Когда новый SW взял управление — один раз перезагружаем страницу,
    // чтобы приложение перешло на свежую сборку
    let reloading = false;
    navigator.serviceWorker.addEventListener('controllerchange', () => {
      if (!hadController || reloading) return;
      reloading = true;
      window.location.reload();
    });
  });
}