import React, { useState, useEffect, useCallback } from 'react';
import { useHistory } from 'react-router-dom';
import { usersAPI } from './api';
import './AdminPage.css';

function AdminPage() {
  const [users, setUsers] = useState([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState('');
  const [user, setUser] = useState(null);

  // Форма создания
  const [showForm, setShowForm] = useState(false);
  const [formData, setFormData] = useState({ username: '', password: '', full_name: '', role: 'user' });
  const [formError, setFormError] = useState('');
  const [formSuccess, setFormSuccess] = useState('');

  const history = useHistory();

  useEffect(() => {
    const userData = localStorage.getItem('user');
    if (!userData) {
      history.push('/login');
      return;
    }
    const parsed = JSON.parse(userData);
    if (parsed.role !== 'admin') {
      history.push('/dashboard');
      return;
    }
    setUser(parsed);
  }, [history]);

  const loadUsers = useCallback(async () => {
    try {
      setLoading(true);
      setError('');
      const response = await usersAPI.list();
      setUsers(response.data);
    } catch (err) {
      if (err.response?.status === 403) {
        history.push('/dashboard');
      } else {
        setError('Ошибка загрузки пользователей');
      }
    } finally {
      setLoading(false);
    }
  }, [history]);

  useEffect(() => {
    loadUsers();
  }, [loadUsers]);

  const handleDelete = async (userId, username) => {
    if (!window.confirm(`Удалить пользователя "${username}"?`)) return;
    try {
      setError('');
      await usersAPI.delete(userId);
      setUsers(users.filter(u => u.id !== userId));
      setFormSuccess(`Пользователь "${username}" удалён`);
      setTimeout(() => setFormSuccess(''), 3000);
    } catch (err) {
      setError(err.response?.data?.error || 'Ошибка удаления');
    }
  };

  const handleCreate = async (e) => {
    e.preventDefault();
    setFormError('');
    setFormSuccess('');

    if (!formData.username || !formData.password || !formData.full_name) {
      setFormError('Заполните все поля');
      return;
    }
    if (formData.password.length < 4) {
      setFormError('Пароль минимум 4 символа');
      return;
    }

    try {
      await usersAPI.create(formData);
      setFormSuccess(`Пользователь "${formData.username}" создан`);
      setFormData({ username: '', password: '', full_name: '', role: 'user' });
      setShowForm(false);
      loadUsers();
      setTimeout(() => setFormSuccess(''), 3000);
    } catch (err) {
      setFormError(err.response?.data?.error || 'Ошибка создания');
    }
  };

  const handleLogout = () => {
    localStorage.removeItem('token');
    localStorage.removeItem('user');
    history.push('/login');
  };

  if (!user) return null;

  return (
    <div className="admin-container">
      <header className="admin-header">
        <div className="header-left">
          <h1>СКУД</h1>
          <span className="header-divider">|</span>
          <span className="user-name">{user.full_name}</span>
          <span className="user-role">(Администратор)</span>
        </div>
        <div className="header-right">
          <button className="nav-button" onClick={() => history.push('/dashboard')}>
            QR-код
          </button>
          <button className="logout-button" onClick={handleLogout}>
            Выйти
          </button>
        </div>
      </header>

      <main className="admin-main">
        <div className="admin-card">
          <div className="admin-card-header">
            <h2>Управление пользователями</h2>
            <button className="add-button" onClick={() => setShowForm(!showForm)}>
              {showForm ? '× Отмена' : '+ Добавить'}
            </button>
          </div>

          {error && <div className="error-message">{error}</div>}
          {formSuccess && <div className="success-message">{formSuccess}</div>}

          {showForm && (
            <form className="user-form" onSubmit={handleCreate}>
              <h3>Новый пользователь</h3>
              {formError && <div className="form-error">{formError}</div>}
              <div className="form-row">
                <div className="form-group">
                  <label>Логин</label>
                  <input
                    type="text"
                    value={formData.username}
                    onChange={e => setFormData({...formData, username: e.target.value})}
                    placeholder="Например: ivanov"
                    required
                  />
                </div>
                <div className="form-group">
                  <label>Пароль</label>
                  <input
                    type="password"
                    value={formData.password}
                    onChange={e => setFormData({...formData, password: e.target.value})}
                    placeholder="Минимум 4 символа"
                    required
                  />
                </div>
              </div>
              <div className="form-row">
                <div className="form-group">
                  <label>ФИО</label>
                  <input
                    type="text"
                    value={formData.full_name}
                    onChange={e => setFormData({...formData, full_name: e.target.value})}
                    placeholder="Иванов Иван Иванович"
                    required
                  />
                </div>
                <div className="form-group">
                  <label>Роль</label>
                  <select
                    value={formData.role}
                    onChange={e => setFormData({...formData, role: e.target.value})}
                  >
                    <option value="user">Пользователь</option>
                    <option value="admin">Администратор</option>
                  </select>
                </div>
              </div>
              <button type="submit" className="submit-button">Создать пользователя</button>
            </form>
          )}

          <div className="users-table-wrapper">
            <table className="users-table">
              <thead>
                <tr>
                  <th>ID</th>
                  <th>Логин</th>
                  <th>ФИО</th>
                  <th>Роль</th>
                  <th>Дата создания</th>
                  <th>Действия</th>
                </tr>
              </thead>
              <tbody>
                {loading ? (
                  <tr><td colSpan="6" className="table-empty">Загрузка...</td></tr>
                ) : users.length === 0 ? (
                  <tr><td colSpan="6" className="table-empty">Нет пользователей</td></tr>
                ) : (
                  users.map(u => (
                    <tr key={u.id}>
                      <td>{u.id}</td>
                      <td><strong>{u.username}</strong></td>
                      <td>{u.full_name}</td>
                      <td>
                        <span className={`role-badge ${u.role}`}>
                          {u.role === 'admin' ? 'Админ' : 'Пользователь'}
                        </span>
                      </td>
                      <td>{new Date(u.created_at).toLocaleDateString('ru-RU')}</td>
                      <td>
                        <button
                          className="delete-button"
                          onClick={() => handleDelete(u.id, u.username)}
                          disabled={u.id === user.id}
                          title={u.id === user.id ? 'Нельзя удалить себя' : 'Удалить'}
                        >
                          {u.id === user.id ? '—' : '✕'}
                        </button>
                      </td>
                    </tr>
                  ))
                )}
              </tbody>
            </table>
          </div>
        </div>
      </main>
    </div>
  );
}

export default AdminPage;