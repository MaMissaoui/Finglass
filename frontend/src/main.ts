import { mount } from 'svelte';
import App from './App.svelte';
import './styles/tokens.css';
import './styles/app.css';

const target = document.getElementById('app');
if (!target) {
  throw new Error('missing #app element');
}

// The gallery only exists in the mock build, the same way api.ts keeps the
// mock API out of the production bundle.
if (import.meta.env.MODE === 'mock') {
  if (location.hash === '#gallery') {
    void import('./gallery/main').then(({ mountGallery }) => mountGallery(target));
  } else {
    mount(App, { target });
  }
} else {
  mount(App, { target });
}
