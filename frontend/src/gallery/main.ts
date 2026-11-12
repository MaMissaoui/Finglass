import { mount } from 'svelte';
import Gallery from './Gallery.svelte';

export function mountGallery(target: HTMLElement): void {
  mount(Gallery, { target });
}
