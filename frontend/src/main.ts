import { mount } from 'svelte';
import App from './App.svelte';
import './styles/tokens.css';
import './styles/app.css';

const target = document.getElementById('app');
if (!target) {
  throw new Error('missing #app element');
}

mount(App, { target });
