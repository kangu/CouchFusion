# Why Vue.js and not React?

## How does the content framework with Vue components served through CouchDB docs work?

Vue ships an optional runtime compiler, @vue/compiler-dom which can take a template string
and compile it into a render function *on the fly*.

Example:

```
import { compile } from '@vue/compiler-dom'
import { h, createApp } from 'vue'

const ast = {
  type: 1,
  tag: 'div',
  children: [
    {
      type: 5,
      content: { type: 4, content: 'msg' }
    }
  ]
}

const { code } = compile(ast)
const render = new Function('Vue', code)({ h })

createApp({ data: () => ({ msg: 'Hello' }), render }).mount('#app')
```

This is possible because Vue was explicitly designed for dynamic template compilation — it has a runtime compiler that can transform an AST → render function → DOM updates, all in the browser (and also server-rendered through Nuxt).

## Why It Doesn’t Work the Same in React

React, in contrast, does not have a runtime compiler.

JSX is just syntax sugar for function calls.
All the transformation from JSX → JavaScript happens at build time, using Babel or SWC.

So React receives plain JavaScript functions — not ASTs or templates.

If you tried to feed React an AST, it wouldn’t know what to do with it.

Example:

```
function App() {
  return React.createElement("div", null, "Hello")
}
```

This function is what React expects — not a structure describing what to render.

## Overall considerations of Vue over React

- I appreciate the stability of the API and best practices across multiple development cycles
- The community-driven development approach is (I think) superior to frameworks supported by large
tech companies like React and Angular.
- Less boilerplate code
- Nuxt is practically just a cleaner and (arguably) better version of Next.js (as it was originally intended).