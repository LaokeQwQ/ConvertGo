import { afterEach, vi } from 'vitest'
import { cleanup } from '@testing-library/svelte'

Object.defineProperty(window, 'matchMedia', {
  writable: true,
  value: (query: string) => ({
    matches: false, media: query, onchange: null,
    addListener() {}, removeListener() {},
    addEventListener() {}, removeEventListener() {}, dispatchEvent() { return true },
  }),
})
	Object.defineProperty(window, 'runtime', {
	  configurable: true,
	  value: {
	    EventsOnMultiple: () => () => {},
	    EventsOff() {},
	    EventsOffAll() {},
	  },
	})

afterEach(() => {
  cleanup()
  vi.restoreAllMocks()
  document.documentElement.removeAttribute('style')
  delete document.documentElement.dataset.theme
})
