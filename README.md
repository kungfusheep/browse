# Browse

A terminal web browser that reimagines the web for monospace displays. Content-first, keyboard-driven, inspired by 70s-80s technical documentation aesthetics.

## Install

```bash
go build
./browse [url]
```

## Usage

```bash
./browse                        # Landing page
./browse https://example.com    # Open URL
```

## Key Bindings

| Key | Action |
|-----|--------|
| `j/k` | Scroll down/up |
| `d/u` | Half-page down/up |
| `g/G` | Top/bottom |
| `f` | Follow link (labels appear) |
| `t` | Table of contents |
| `n` | Navigation overlay |
| `o` | Open URL |
| `b/B` | Back/forward |
| `H` | Home |
| `i` | Input mode (forms) |
| `w` | Toggle wide mode |
| `s` | DOM inspector |
| `r` | Reload with JS (headless Chrome) |
| `y` | Copy URL |
| `q` | Quit |

## Features

- Clean text rendering with proper wrapping and justification
- Box-drawn tables
- RFC-style section numbering
- Link following with smart labels
- Form submission
- History navigation
- JavaScript rendering via chromedp
- AI-powered site transformation rules (experimental)
- LaTeX/math rendering

## Design Philosophy

- **Text-first** — not trying to recreate web layouts, reimagining them
- **100% content** — no chrome eating screen space
- **Beautiful monospace** — layouts that look great in fixed-width fonts
- **Keyboard-driven** — vim-style, fast, natural

## License

MIT
