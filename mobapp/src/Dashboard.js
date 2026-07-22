import React, { useState, useEffect, useCallback, useRef } from 'react';
import { useHistory } from 'react-router-dom';
import { QRCodeSVG } from 'qrcode.react';
import { qrAPI } from './api';
import './Dashboard.css';

const REFRESH_INTERVAL = 10; // seconds

function Dashboard() {
  const [user, setUser] = useState(null);
  const [qrToken, setQrToken] = useState('');
  const [qrData, setQrData] = useState('');
  const [timeLeft, setTimeLeft] = useState(REFRESH_INTERVAL);
  const [generatedAt, setGeneratedAt] = useState('');
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState('');
  const history = useHistory();
  const timerRef = useRef(null);

  useEffect(() => {
    const userData = localStorage.getItem('user');
    if (!userData) {
      history.push('/login');
      return;
    }
    setUser(JSON.parse(userData));
  }, [history]);

  const generateQR = useCallback(async () => {
    setLoading(true);
    setError('');
    try {
      const response = await qrAPI.generateQR();
      setQrToken(response.data.qr_code);
      setQrData(response.data.user_data);
      setGeneratedAt(response.data.generated_at);
      setTimeLeft(REFRESH_INTERVAL);
    } catch (err) {
      if (err.response?.status !== 401) {
        setError('Ошибка генерации QR-кода');
      }
    } finally {
      setLoading(false);
    }
  }, []);

  // Auto-generate QR on mount
  useEffect(() => {
    generateQR();
  }, [generateQR]);

  // Timer for QR refresh
  useEffect(() => {
    timerRef.current = setInterval(() => {
      setTimeLeft((prev) => {
        if (prev <= 1) {
          generateQR();
          return REFRESH_INTERVAL;
        }
        return prev - 1;
      });
    }, 1000);

    return () => {
      if (timerRef.current) {
        clearInterval(timerRef.current);
      }
    };
  }, [generateQR]);

  const handleLogout = () => {
    localStorage.removeItem('token');
    localStorage.removeItem('user');
    history.push('/login');
  };

  const formatTime = (dateStr) => {
    if (!dateStr) return '';
    const d = new Date(dateStr);
    return d.toLocaleTimeString('ru-RU', { hour12: false });
  };

  if (!user) return null;

  return (
    <div className="dashboard-container">
      <header className="dashboard-header">
        <div className="header-left">
          <h1>EnterID</h1>
          <span className="header-divider">|</span>
          <span className="user-name">{user.full_name}</span>
          <span className="user-role">{user.role === 'admin' ? '(Администратор)' : '(Пользователь)'}</span>
        </div>
        <div className="header-right">
          {user.role === 'admin' && (
            <button className="nav-button" onClick={() => history.push('/admin')}>
              Управление
            </button>
          )}
          <button className="logout-button" onClick={handleLogout}>
            Выйти
          </button>
        </div>
      </header>

      <main className="dashboard-main">
        <div className="qr-section">
          <div className="qr-card">
            <div className="qr-card-header">
              <h2>QR-код доступа</h2>
              <div className="qr-timer">
                <div className="timer-ring" style={{
                  '--progress': `${(timeLeft / REFRESH_INTERVAL) * 100}%`
                }}>
                  <span className="timer-value">{timeLeft}</span>
                  <span className="timer-label">сек</span>
                </div>
              </div>
            </div>

            <div className="qr-code-wrapper">
              {loading && !qrToken ? (
                <div className="qr-loading">Генерация...</div>
              ) : qrToken ? (
                <QRCodeSVG
                  value={qrToken}
                  size={280}
                  level="H"
                  bgColor="#ffffff"
                  fgColor="#000000"
                />
              ) : null}
            </div>

            {error && <div className="qr-error">{error}</div>}

            {generatedAt && (
              <div className="qr-info">
                <div className="info-row">
                  <span className="info-label">Сгенерирован:</span>
                  <span className="info-value">{formatTime(generatedAt)}</span>
                </div>
                <div className="info-row">
                  <span className="info-label">Действителен:</span>
                  <span className="info-value">{REFRESH_INTERVAL} секунд</span>
                </div>
              </div>
            )}
          </div>

          <div className="qr-token-section">
            <div className="token-card">
              <h3>Токен QR-кода</h3>
              <div className="token-value">
                {qrToken || '—'}
              </div>
            </div>
          </div>
        </div>
      </main>
    </div>
  );
}

export default Dashboard;