// Package latex converts LaTeX math notation to Unicode for terminal display.
package latex

import (
	"regexp"
	"strings"
)

// ToUnicode converts LaTeX math notation to Unicode characters.
func ToUnicode(latex string) string {
	s := latex

	// Remove display math delimiters
	s = strings.TrimPrefix(s, "\\[")
	s = strings.TrimSuffix(s, "\\]")
	s = strings.TrimPrefix(s, "$$")
	s = strings.TrimSuffix(s, "$$")
	s = strings.TrimPrefix(s, "$")
	s = strings.TrimSuffix(s, "$")
	s = strings.TrimPrefix(s, "\\(")
	s = strings.TrimSuffix(s, "\\)")
	s = strings.TrimSpace(s)

	// Process in order: commands first, then super/subscripts
	s = replaceCommands(s)
	s = processSuperscripts(s)
	s = processSubscripts(s)
	s = cleanupSpacing(s)

	return s
}

// Greek letters (lowercase)
var greekLower = map[string]string{
	"\\alpha":   "α", "\\beta": "β", "\\gamma": "γ", "\\delta": "δ",
	"\\epsilon": "ε", "\\varepsilon": "ε", "\\zeta": "ζ", "\\eta": "η",
	"\\theta": "θ", "\\vartheta": "ϑ", "\\iota": "ι", "\\kappa": "κ",
	"\\lambda": "λ", "\\mu": "μ", "\\nu": "ν", "\\xi": "ξ",
	"\\pi": "π", "\\varpi": "ϖ", "\\rho": "ρ", "\\varrho": "ϱ",
	"\\sigma": "σ", "\\varsigma": "ς", "\\tau": "τ", "\\upsilon": "υ",
	"\\phi": "φ", "\\varphi": "ϕ", "\\chi": "χ", "\\psi": "ψ", "\\omega": "ω",
}

// Greek letters (uppercase)
var greekUpper = map[string]string{
	"\\Gamma": "Γ", "\\Delta": "Δ", "\\Theta": "Θ", "\\Lambda": "Λ",
	"\\Xi": "Ξ", "\\Pi": "Π", "\\Sigma": "Σ", "\\Upsilon": "Υ",
	"\\Phi": "Φ", "\\Psi": "Ψ", "\\Omega": "Ω",
}

// Math operators and symbols
var mathSymbols = map[string]string{
	// Operators
	"\\cdot": "·", "\\times": "×", "\\div": "÷", "\\pm": "±", "\\mp": "∓",
	"\\ast": "∗", "\\star": "⋆", "\\circ": "∘", "\\bullet": "•",

	// Relations
	"\\leq": "≤", "\\le": "≤", "\\geq": "≥", "\\ge": "≥",
	"\\neq": "≠", "\\ne": "≠", "\\approx": "≈", "\\equiv": "≡",
	"\\sim": "∼", "\\simeq": "≃", "\\cong": "≅", "\\propto": "∝",
	"\\ll": "≪", "\\gg": "≫", "\\prec": "≺", "\\succ": "≻",

	// Arrows
	"\\rightarrow": "→", "\\to": "→", "\\leftarrow": "←", "\\gets": "←",
	"\\leftrightarrow": "↔", "\\Rightarrow": "⇒", "\\Leftarrow": "⇐",
	"\\Leftrightarrow": "⇔", "\\mapsto": "↦", "\\uparrow": "↑", "\\downarrow": "↓",
	"\\nearrow": "↗", "\\searrow": "↘", "\\swarrow": "↙", "\\nwarrow": "↖",
	"\\implies": "⟹", "\\impliedby": "⟸", "\\iff": "⟺",
	"\\longrightarrow": "⟶", "\\longleftarrow": "⟵", "\\longmapsto": "⟼",
	"\\Downarrow": "⇓", "\\Uparrow": "⇑", "\\updownarrow": "↕", "\\Updownarrow": "⇕",

	// Set theory
	"\\in": "∈", "\\notin": "∉", "\\ni": "∋", "\\subset": "⊂", "\\supset": "⊃",
	"\\subseteq": "⊆", "\\supseteq": "⊇", "\\cup": "∪", "\\cap": "∩",
	"\\emptyset": "∅", "\\varnothing": "∅",

	// Logic
	"\\land": "∧", "\\wedge": "∧", "\\lor": "∨", "\\vee": "∨",
	"\\neg": "¬", "\\lnot": "¬", "\\forall": "∀", "\\exists": "∃",
	"\\nexists": "∄", "\\therefore": "∴", "\\because": "∵",

	// Calculus & Analysis
	"\\infty": "∞", "\\partial": "∂", "\\nabla": "∇",
	"\\sum": "∑", "\\prod": "∏", "\\coprod": "∐",
	"\\int": "∫", "\\iint": "∬", "\\iiint": "∭", "\\oint": "∮",

	// Misc symbols
	"\\sqrt": "√", "\\surd": "√", "\\prime": "′", "\\degree": "°",
	"\\angle": "∠", "\\triangle": "△", "\\square": "□", "\\diamond": "◇",
	"\\aleph": "ℵ", "\\hbar": "ℏ", "\\ell": "ℓ", "\\wp": "℘",
	"\\Re": "ℜ", "\\Im": "ℑ", "\\complement": "∁",

	// Dots
	"\\ldots": "…", "\\cdots": "⋯", "\\vdots": "⋮", "\\ddots": "⋱",

	// Brackets (when used as commands)
	"\\langle": "⟨", "\\rangle": "⟩", "\\lceil": "⌈", "\\rceil": "⌉",
	"\\lfloor": "⌊", "\\rfloor": "⌋", "\\lvert": "|", "\\rvert": "|",
	"\\|": "‖", "\\lVert": "‖", "\\rVert": "‖",
}

// Spacing commands
var spacingCommands = map[string]string{
	"\\,": " ", "\\:": " ", "\\;": " ", "\\ ": " ",
	"\\quad": "  ", "\\qquad": "    ",
}

// Text/math mode commands that just pass through content
var textCommands = []string{
	"\\mathrm", "\\text", "\\textrm", "\\textit", "\\textbf",
	"\\mathit", "\\mathbf", "\\mathsf", "\\mathtt", "\\mathcal",
	"\\operatorname", "\\mod", "\\bmod", "\\pmod",
}

// Superscript characters
var superscripts = map[rune]rune{
	'0': '⁰', '1': '¹', '2': '²', '3': '³', '4': '⁴',
	'5': '⁵', '6': '⁶', '7': '⁷', '8': '⁸', '9': '⁹',
	'+': '⁺', '-': '⁻', '=': '⁼', '(': '⁽', ')': '⁾',
	'a': 'ᵃ', 'b': 'ᵇ', 'c': 'ᶜ', 'd': 'ᵈ', 'e': 'ᵉ',
	'f': 'ᶠ', 'g': 'ᵍ', 'h': 'ʰ', 'i': 'ⁱ', 'j': 'ʲ',
	'k': 'ᵏ', 'l': 'ˡ', 'm': 'ᵐ', 'n': 'ⁿ', 'o': 'ᵒ',
	'p': 'ᵖ', 'r': 'ʳ', 's': 'ˢ', 't': 'ᵗ', 'u': 'ᵘ',
	'v': 'ᵛ', 'w': 'ʷ', 'x': 'ˣ', 'y': 'ʸ', 'z': 'ᶻ',
	'A': 'ᴬ', 'B': 'ᴮ', 'D': 'ᴰ', 'E': 'ᴱ', 'G': 'ᴳ',
	'H': 'ᴴ', 'I': 'ᴵ', 'J': 'ᴶ', 'K': 'ᴷ', 'L': 'ᴸ',
	'M': 'ᴹ', 'N': 'ᴺ', 'O': 'ᴼ', 'P': 'ᴾ', 'R': 'ᴿ',
	'T': 'ᵀ', 'U': 'ᵁ', 'V': 'ⱽ', 'W': 'ᵂ',
}

// Subscript characters
var subscripts = map[rune]rune{
	'0': '₀', '1': '₁', '2': '₂', '3': '₃', '4': '₄',
	'5': '₅', '6': '₆', '7': '₇', '8': '₈', '9': '₉',
	'+': '₊', '-': '₋', '=': '₌', '(': '₍', ')': '₎',
	'a': 'ₐ', 'e': 'ₑ', 'h': 'ₕ', 'i': 'ᵢ', 'j': 'ⱼ',
	'k': 'ₖ', 'l': 'ₗ', 'm': 'ₘ', 'n': 'ₙ', 'o': 'ₒ',
	'p': 'ₚ', 'r': 'ᵣ', 's': 'ₛ', 't': 'ₜ', 'u': 'ᵤ',
	'v': 'ᵥ', 'x': 'ₓ',
}

func replaceCommands(s string) string {
	// Handle LaTeX environments first - strip \begin{...} and \end{...}
	envRe := regexp.MustCompile(`\\begin\{[^}]*\}`)
	s = envRe.ReplaceAllString(s, "")
	endEnvRe := regexp.MustCompile(`\\end\{[^}]*\}`)
	s = endEnvRe.ReplaceAllString(s, "")

	// Handle alignment markers: & becomes space, \\ becomes newline
	s = strings.ReplaceAll(s, "\\\\", "\n")
	s = strings.ReplaceAll(s, "&", " ")

	// Handle \hspace{...}, \vspace{...}, \phantom{...} - remove them
	hspaceRe := regexp.MustCompile(`\\hspace\{[^}]*\}`)
	s = hspaceRe.ReplaceAllString(s, " ")
	vspaceRe := regexp.MustCompile(`\\vspace\{[^}]*\}`)
	s = vspaceRe.ReplaceAllString(s, "")
	phantomRe := regexp.MustCompile(`\\phantom\{[^}]*\}`)
	s = phantomRe.ReplaceAllString(s, "")

	// Handle \color{...} and {\color{...} text} - just keep the text
	colorRe := regexp.MustCompile(`\\color\{[^}]*\}`)
	s = colorRe.ReplaceAllString(s, "")
	// Also handle \textcolor{color}{text} -> text
	textcolorRe := regexp.MustCompile(`\\textcolor\{[^}]*\}\{([^}]*)\}`)
	s = textcolorRe.ReplaceAllString(s, "$1")

	// Handle \left and \right FIRST (before \le/\leq can corrupt them)
	// These are size modifiers for brackets
	s = strings.ReplaceAll(s, "\\left(", "(")
	s = strings.ReplaceAll(s, "\\right)", ")")
	s = strings.ReplaceAll(s, "\\left[", "[")
	s = strings.ReplaceAll(s, "\\right]", "]")
	s = strings.ReplaceAll(s, "\\left\\{", "{")
	s = strings.ReplaceAll(s, "\\right\\}", "}")
	s = strings.ReplaceAll(s, "\\left{", "{")
	s = strings.ReplaceAll(s, "\\right}", "}")
	s = strings.ReplaceAll(s, "\\left|", "|")
	s = strings.ReplaceAll(s, "\\right|", "|")
	s = strings.ReplaceAll(s, "\\left.", "")
	s = strings.ReplaceAll(s, "\\right.", "")
	s = strings.ReplaceAll(s, "\\left\\langle", "⟨")
	s = strings.ReplaceAll(s, "\\right\\rangle", "⟩")

	// Handle \sqrt{...} with braces
	sqrtRe := regexp.MustCompile(`\\sqrt\{([^}]*)\}`)
	s = sqrtRe.ReplaceAllString(s, "√($1)")

	// Handle \frac{a}{b}
	fracRe := regexp.MustCompile(`\\frac\{([^}]*)\}\{([^}]*)\}`)
	s = fracRe.ReplaceAllString(s, "($1)/($2)")

	// Handle text commands like \mathrm{mod} -> mod
	for _, cmd := range textCommands {
		re := regexp.MustCompile(regexp.QuoteMeta(cmd) + `\{([^}]*)\}`)
		s = re.ReplaceAllString(s, "$1")
	}

	// Handle \mathbb{R} -> R (blackboard bold - just use regular letter)
	mathbbRe := regexp.MustCompile(`\\mathbb\{([^}]*)\}`)
	s = mathbbRe.ReplaceAllString(s, "$1")

	// Collect all commands and sort by length (longest first)
	// This prevents \le from matching before \leq
	allCommands := make(map[string]string)
	for k, v := range greekLower {
		allCommands[k] = v
	}
	for k, v := range greekUpper {
		allCommands[k] = v
	}
	for k, v := range mathSymbols {
		allCommands[k] = v
	}
	for k, v := range spacingCommands {
		allCommands[k] = v
	}

	// Sort commands by length (longest first)
	sortedCmds := sortByLengthDesc(allCommands)
	for _, cmd := range sortedCmds {
		s = strings.ReplaceAll(s, cmd, allCommands[cmd])
	}

	// Handle escaped characters
	s = strings.ReplaceAll(s, "\\{", "{")
	s = strings.ReplaceAll(s, "\\}", "}")
	s = strings.ReplaceAll(s, "\\%", "%")
	s = strings.ReplaceAll(s, "\\$", "$")
	s = strings.ReplaceAll(s, "\\&", "&")
	s = strings.ReplaceAll(s, "\\_", "_")

	return s
}

// sortByLengthDesc returns keys sorted by length (longest first)
func sortByLengthDesc(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	// Simple bubble sort by length descending
	for i := 0; i < len(keys)-1; i++ {
		for j := i + 1; j < len(keys); j++ {
			if len(keys[j]) > len(keys[i]) {
				keys[i], keys[j] = keys[j], keys[i]
			}
		}
	}
	return keys
}

func processSuperscripts(s string) string {
	// Handle ^{...} groups first
	braceRe := regexp.MustCompile(`\^\{([^}]*)\}`)
	s = braceRe.ReplaceAllStringFunc(s, func(match string) string {
		inner := match[2 : len(match)-1] // Remove ^{ and }
		return toSuperscript(inner)
	})

	// Handle ^x (single character)
	singleRe := regexp.MustCompile(`\^([a-zA-Z0-9+\-=()])`)
	s = singleRe.ReplaceAllStringFunc(s, func(match string) string {
		char := rune(match[1])
		if sup, ok := superscripts[char]; ok {
			return string(sup)
		}
		return "^" + string(char)
	})

	return s
}

func processSubscripts(s string) string {
	// Handle _{...} groups first
	braceRe := regexp.MustCompile(`_\{([^}]*)\}`)
	s = braceRe.ReplaceAllStringFunc(s, func(match string) string {
		inner := match[2 : len(match)-1] // Remove _{ and }
		return toSubscript(inner)
	})

	// Handle _x (single character)
	singleRe := regexp.MustCompile(`_([a-zA-Z0-9+\-=()])`)
	s = singleRe.ReplaceAllStringFunc(s, func(match string) string {
		char := rune(match[1])
		if sub, ok := subscripts[char]; ok {
			return string(sub)
		}
		return "_" + string(char)
	})

	return s
}

func toSuperscript(s string) string {
	var result strings.Builder
	for _, r := range s {
		if sup, ok := superscripts[r]; ok {
			result.WriteRune(sup)
		} else {
			// Fall back to keeping the character
			result.WriteRune(r)
		}
	}
	return result.String()
}

func toSubscript(s string) string {
	var result strings.Builder
	for _, r := range s {
		if sub, ok := subscripts[r]; ok {
			result.WriteRune(sub)
		} else {
			// Fall back to keeping the character
			result.WriteRune(r)
		}
	}
	return result.String()
}

func cleanupSpacing(s string) string {
	// Collapse multiple spaces
	spaceRe := regexp.MustCompile(`\s+`)
	s = spaceRe.ReplaceAllString(s, " ")
	return strings.TrimSpace(s)
}

// ContainsLaTeX checks if a string contains LaTeX math notation.
func ContainsLaTeX(s string) bool {
	// Check for common LaTeX delimiters
	if strings.Contains(s, "\\[") || strings.Contains(s, "\\]") {
		return true
	}
	if strings.Contains(s, "$$") {
		return true
	}
	if strings.Contains(s, "\\(") || strings.Contains(s, "\\)") {
		return true
	}
	// Check for $ delimiters (but not \$)
	dollarRe := regexp.MustCompile(`(?:^|[^\\])\$`)
	return dollarRe.MatchString(s)
}

// ProcessText finds and converts all LaTeX math in a string.
func ProcessText(s string) string {
	// Process display math \[...\] ((?s) makes . match newlines)
	displayRe := regexp.MustCompile(`(?s)\\\[(.*?)\\\]`)
	s = displayRe.ReplaceAllStringFunc(s, func(match string) string {
		return ToUnicode(match)
	})

	// Process display math $$...$$ ((?s) makes . match newlines)
	doubleDollarRe := regexp.MustCompile(`(?s)\$\$(.*?)\$\$`)
	s = doubleDollarRe.ReplaceAllStringFunc(s, func(match string) string {
		return ToUnicode(match)
	})

	// Process inline math \(...\)
	inlineParenRe := regexp.MustCompile(`(?s)\\\((.*?)\\\)`)
	s = inlineParenRe.ReplaceAllStringFunc(s, func(match string) string {
		return ToUnicode(match)
	})

	// Process inline math $...$ (but not \$)
	// This is trickier - need to avoid escaped dollars
	inlineDollarRe := regexp.MustCompile(`(?:^|[^\\])\$([^$]+)\$`)
	s = inlineDollarRe.ReplaceAllStringFunc(s, func(match string) string {
		// Preserve any leading character before the $
		prefix := ""
		if len(match) > 0 && match[0] != '$' {
			prefix = string(match[0])
			match = match[1:]
		}
		return prefix + ToUnicode(match)
	})

	return s
}
