import { mount } from 'svelte'
import App from './App.svelte'
import './app.css'

const haColors = {
  '--bg': '--primary-background-color',
  '--panel': '--card-background-color',
  '--text': '--primary-text-color',
  '--muted': '--secondary-text-color',
  '--line': '--divider-color',
  '--accent': '--primary-color',
}

try {
  const theme = getComputedStyle(parent.document.documentElement)
  for (const [token, name] of Object.entries(haColors)) {
    const value = theme.getPropertyValue(name).trim()
    if (value) document.documentElement.style.setProperty(token, value)
  }
} catch {
  // HA's page isn't readable, so keep the colours from app.css.
}

mount(App, { target: document.getElementById('app') })
