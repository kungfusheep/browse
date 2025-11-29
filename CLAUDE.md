# Browse - Terminal Web Browser

## Design Goals

### Core Philosophy
- **Text-first**: We're not trying to recreate web pages visually - we're reimagining them for the terminal
- **Beautiful monospace**: Layouts that look *amazing* in monospaced fonts
- **Navigation ease**: Getting around should feel natural and fast
- **Readability over fidelity**: Content clarity trumps visual accuracy to the original

### Visual Principles
- Clean, well-structured text layouts
- Thoughtful use of whitespace
- Smart text wrapping that respects terminal width
- Clear visual hierarchy using terminal capabilities (bold, colors, spacing)

### Navigation Principles
- Keyboard-driven (vim-style bindings welcome)
- Quick link jumping
- History navigation
- Bookmarks

### What We're NOT Doing
- Rendering CSS layouts faithfully
- JavaScript execution
- Complex grid/flexbox recreation
- Image rendering (maybe ASCII art later, but not a priority)

## Technical Decisions
- Language: Go
- TUI framework: **None** - we own the rendering layer completely
- HTML parsing: TBD

## Visual Theme: 70s-80s Technical Documentation
Think DEC manuals, IBM technical references, academic papers of the era.

### Character Elements
- Box drawing: `┌─┐│└┘├┬┤┴┼` (and double-line variants `╔═╗║╚╝`)
- Horizontal rules and section breaks
- Block elements for emphasis where appropriate

### Text Treatment
- Justified text where possible (proper word spacing)
- Clear hierarchical headings
- Indentation for structure
- Dense but readable information layout

### Tables
- Proper box-drawn tables
- Column alignment (left, right, center as appropriate)
- Header rows with visual distinction

### Overall Feel
- Professional, authoritative, timeless
- Information-dense but never cluttered
- The satisfying aesthetic of well-typeset technical prose

### UI Philosophy
- 100% content by default - no chrome eating screen real estate
- Visual elements (command input, status, settings) appear only when needed
- Content is king; navigation and controls are servants

## Current Focus
- Building the core rendering engine
