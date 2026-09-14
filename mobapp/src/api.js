import axios from 'axios';

// Auto-detect API host:
// 1. Environment variable (REACT_APP_API_URL) takes priority
// 2. Otherwise use the same host that served the frontend (works on LAN)
//    e.g. if page opened from http://192.168.0.126:3000, API = http://192.168.0.126:8080/api
const detectApiBase = () => {
  if (process.env.REACT_APP_API_URL || process.env.VITE_API_URL) {
    return process.env.REACT_APP_API_URL || process.env.VITE_API_URL;
  }

  // Same origin: the HTTPS server started by serve.sh proxies /api/*
  // to the Go backend on :8080. This avoids mixed-content blocking when the
  // frontend is served over HTTPS and works with any host/port.
  return `${window.location.origin}/api`;
};

const API_BASE = detectApiBase();

const api = axios.create({
  baseURL: API_BASE,
  headers: {
    'Content-Type': 'application/json',
  },
  timeout: 10000,
});

// Add auth token to requests
api.interceptors.request.use((config) => {
  const token = localStorage.getItem('token');
  if (token) {
    config.headers.Authorization = `Bearer ${token}`;
  }
  return config;
});

// Handle auth errors
api.interceptors.response.use(
  (response) => response,
  (error) => {
    if (error.response?.status === 401) {
      localStorage.removeItem('token');
      localStorage.removeItem('user');
      window.location.href = '/login';
    }
    return Promise.reject(error);
  }
);

export const authAPI = {
  login: (username, password) => api.post('/login', { username, password }),
  getProfile: () => api.get('/profile'),
};

export const qrAPI = {
  generateQR: () => api.post('/generate-qr'),
  getPublicKey: () => api.get('/public-key'),
  getAccessLogs: () => api.get('/access-logs'),
};

export const scannerAPI = {
  validateQR: (token) => api.post('/validate-qr', { token }),
};

export const usersAPI = {
  list: () => api.get('/users'),
  get: (id) => api.get(`/users/${id}`),
  create: (data) => api.post('/users', data),
  update: (id, data) => api.put(`/users/${id}`, data),
  delete: (id) => api.delete(`/users/${id}`),
};

export default api;