import './style.css'
import { mount } from 'svelte'
import App from './App.svelte'

const target = document.getElementById('app')
if (!target) throw new Error('ConvertGo application root is missing')
const app = mount(App, { target })

export default app
