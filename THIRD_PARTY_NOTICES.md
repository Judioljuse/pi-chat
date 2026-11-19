# Third-party notices

The web UI (`web-src/`, built into `web/dist/`) includes source code copied from the component libraries below. Each copy was restyled to pichat's design tokens (see `web-src/DESIGN.md`). Libraries installed from npm keep their own license files in `node_modules` and are listed at the end.

## shadcn/ui

- Source: https://ui.shadcn.com (registry style `radix-nova`, installed with `npx shadcn@latest add`)
- License: MIT
- Files: `web-src/src/components/ui/*`

```
MIT License

Copyright (c) 2023 shadcn

Permission is hereby granted, free of charge, to any person obtaining a copy
of this software and associated documentation files (the "Software"), to deal
in the Software without restriction, including without limitation the rights
to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
copies of the Software, and to permit persons to whom the Software is
furnished to do so, subject to the following conditions:

The above copyright notice and this permission notice shall be included in all
copies or substantial portions of the Software.

THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
SOFTWARE.
```

## beUI

- Source: https://beui.dev (registry items `https://beui.dev/r/<name>.json`: message, message-bubble, message-scroller, prompt-input, dropdown-menu, todo-list, agent-activity, notification-stack, collapsible, alert, avatar-group, attachment-upload, plus the registry dependencies they pull in)
- Repository: https://github.com/starc007/ui-components (author: Saurabh Chauhan)
- License: MIT (upstream `LICENSE` file, checked 2026-11-19)
- Files: `web-src/src/components/agents/**`, `web-src/src/components/motion/**`, `web-src/src/lib/{ease.ts,i18n.tsx,presence-gate.tsx,focusable.ts,touch.ts,text-shimmer.ts}`, `web-src/src/lib/hooks/*`
- Changes: the ease curve is set to the Nuphos curve; dropdown timings are retuned; hard-coded palette classes are replaced with theme tokens; one broken import path is fixed (`text-shimmer`); `process.env` is replaced with `import.meta.env`. For the chat view, `prompt-input` gains optional `submitOnEnter`, `canSubmit`, `header` and `textareaRef` props; `attachment-upload` gains `dropzone={false}` to render only the attachment list; `avatar-group` gains `renderAvatar`; the `outline` message bubble uses `bg-card` with the standard border.

```
MIT License

Copyright (c) 2026 Saurabh Chauhan

Permission is hereby granted, free of charge, to any person obtaining a copy
of this software and associated documentation files (the "Software"), to deal
in the Software without restriction, including without limitation the rights
to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
copies of the Software, and to permit persons to whom the Software is
furnished to do so, subject to the following conditions:

The above copyright notice and this permission notice shall be included in all
copies or substantial portions of the Software.

THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
SOFTWARE.
```

## AI Elements (Vercel)

- Source: https://elements.ai-sdk.dev (registry `https://registry.ai-sdk.dev/<name>.json`: shimmer, loader, task, conversation, suggestion)
- Repository: https://github.com/vercel/ai-elements (npm package `ai-elements` 1.9.1). License: Apache-2.0; the notice below is the upstream `LICENSE` file's header, checked 2026-11-19.
- Files: `web-src/src/components/ai-elements/*` (each file starts with a notice that it was modified)
- Changes: restyled to pichat's design tokens (colors, sizes, spacing; see `web-src/DESIGN.md`); imports point at the local shadcn/ui components.
- The full Apache License 2.0 text is in `LICENSES/Apache-2.0.txt`.
- The site says AI Elements is deprecated at the end of November 2026 and is moving into shadcn/ui.

```
Copyright 2023 Vercel, Inc.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
```

## Beautiful UI

- Source: https://www.beautifului.dev (copied with each section's Copy button: Loading State, Thinking, Tool Chips, Task Rows, Code Block)
- License: MIT (https://www.beautifului.dev/license). The site's components are also published at https://github.com/TurboKach/ai-native-react-components, whose `LICENSE` reads "Copyright (c) 2026 Turbo" (MIT). The notice below is the one on the site's license page.
- Files: `web-src/src/components/beautiful/*`
- Changes: site tokens (`--ink`, `--line`, `bg-hover`, ...) are mapped to pichat tokens. Loading State has its Surfer variant removed (it streamed a remote video) and takes a `startedAt` prop for the timer. Prompt Bar was not copied because it depends on the `glimm` shader package. Code Block takes a `className` and falls back to `execCommand('copy')` when `navigator.clipboard` is unavailable (plain-http origins). Task Rows has a static `failed` row status for real run history.

```
MIT License

Copyright (c) 2026 Shane Levine

Permission is hereby granted, free of charge, to any person obtaining a copy
of this software and associated documentation files (the "Software"), to deal
in the Software without restriction, including without limitation the rights
to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
copies of the Software, and to permit persons to whom the Software is
furnished to do so, subject to the following conditions:

The above copyright notice and this permission notice shall be included in all
copies or substantial portions of the Software.

THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
SOFTWARE.
```

## Previously included (removed)

Commits before the React rewrite (from `77d080f` to just before `2712f99`) contained these files under `web/vendor/`; they are no longer part of the tree:

- preact and preact/hooks (`preact.js`, `hooks.js`): MIT, Copyright (c) 2015-present Jason Miller
- htm (`htm.js`): Apache-2.0, Copyright 2018 Google Inc.
- marked 15.0.12 (`marked.js`): MIT, Copyright (c) 2011-2025, Christopher Jeffrey (license header kept in the file)
- DOMPurify 3.4.18 (`purify.js`): Apache-2.0 or MPL-2.0, (c) Cure53 and other contributors (license header kept in the file)

## Not used

- Generative Loaders, AIUI Components: their sites were not reachable from the build machine.
- React Bits Pro: paid code.

## Design reference

The colors, layout and motion follow Nuphos (https://nuphos.com). No Nuphos code is included; only CSS token values read from its public stylesheet were used.

## npm dependencies bundled into `web/dist`

Listed from the build's source maps. License texts are in `web-src/node_modules/<package>/LICENSE` (or `LICENSE.txt`, `license`).

- MIT: react, react-dom, scheduler; radix-ui and the `@radix-ui/*` packages it pulls in; `@floating-ui/core`, `@floating-ui/dom`, `@floating-ui/react-dom`, `@floating-ui/utils`; motion, framer-motion, motion-dom, motion-utils; sonner; cmdk; use-stick-to-bottom; clsx; tailwind-merge; tw-animate-css (CSS); aria-hidden, react-remove-scroll, react-remove-scroll-bar, react-style-singleton, use-callback-ref, use-sidecar, get-nonce; react-markdown, remark-gfm, remark-breaks, remark-parse, remark-rehype, unified, trough, bail, extend, is-plain-obj, vfile, vfile-message, and the `mdast-util-*`, `micromark*`, `hast-util-*`, `unist-util-*` packages they use, plus ccount, comma-separated-tokens, space-separated-tokens, decode-named-character-reference, escape-string-regexp, estree-util-is-identifier-name, html-url-attributes, inline-style-parser, style-to-object, longest-streak, markdown-table, property-information, trim-lines
- ISC: lucide-react (some icons are derived from Feather, MIT, Copyright (c) 2013-present Cole Bemis; see its LICENSE), `@ungap/structured-clone`
- Apache-2.0: class-variance-authority
- 0BSD: tslib
