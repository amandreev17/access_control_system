import React from 'react';
import { BrowserRouter as Router, Route, Switch, Redirect } from 'react-router-dom';
import LoginPage from './LoginPage';
import Dashboard from './Dashboard';
import AdminPage from './AdminPage';
import './App.css';

function PrivateRoute({ component: Component, ...rest }) {
  const token = localStorage.getItem('token');
  const user = localStorage.getItem('user');
  return (
    <Route
      {...rest}
      render={(props) =>
        token && user ? (
          <Component {...props} />
        ) : (
          <Redirect to="/login" />
        )
      }
    />
  );
}

function App() {
  return (
    <Router>
      <div className="App">
        <Switch>
          <Route path="/login" component={LoginPage} />
          <PrivateRoute path="/dashboard" component={Dashboard} />
          <PrivateRoute path="/admin" component={AdminPage} />
          <Redirect from="/" exact to="/login" />
          <Redirect to="/login" />
        </Switch>
      </div>
    </Router>
  );
}

export default App;