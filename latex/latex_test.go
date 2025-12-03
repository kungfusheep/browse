package latex

import "testing"

func TestToUnicode(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		// Basic superscripts
		{"simple superscript", "x^2", "x²"},
		{"superscript group", "x^{10}", "x¹⁰"},
		{"multiple superscripts", "x^2 + y^3", "x² + y³"},

		// Basic subscripts
		{"simple subscript", "x_1", "x₁"},
		{"subscript group", "x_{12}", "x₁₂"},
		{"mixed sub and super", "x_1^2", "x₁²"},

		// Greek letters
		{"alpha", "\\alpha", "α"},
		{"beta", "\\beta", "β"},
		{"pi", "\\pi", "π"},
		{"Sigma", "\\Sigma", "Σ"},
		{"omega", "\\omega", "ω"},

		// Math symbols
		{"infinity", "\\infty", "∞"},
		{"not equal", "\\neq", "≠"},
		{"less or equal", "\\leq", "≤"},
		{"greater or equal", "\\geq", "≥"},
		{"approximately", "\\approx", "≈"},
		{"times", "\\times", "×"},
		{"dot", "\\cdot", "·"},
		{"plus minus", "\\pm", "±"},

		// Arrows
		{"right arrow", "\\rightarrow", "→"},
		{"left arrow", "\\leftarrow", "←"},
		{"implies", "\\Rightarrow", "⇒"},
		{"iff", "\\Leftrightarrow", "⇔"},
		{"maps to", "\\mapsto", "↦"},

		// Set theory
		{"in", "\\in", "∈"},
		{"not in", "\\notin", "∉"},
		{"subset", "\\subset", "⊂"},
		{"union", "\\cup", "∪"},
		{"intersection", "\\cap", "∩"},
		{"empty set", "\\emptyset", "∅"},

		// Logic
		{"forall", "\\forall", "∀"},
		{"exists", "\\exists", "∃"},
		{"and", "\\land", "∧"},
		{"or", "\\lor", "∨"},
		{"not", "\\neg", "¬"},

		// Calculus
		{"sum", "\\sum", "∑"},
		{"product", "\\prod", "∏"},
		{"integral", "\\int", "∫"},
		{"partial", "\\partial", "∂"},
		{"nabla", "\\nabla", "∇"},

		// Square root
		{"sqrt", "\\sqrt{x}", "√(x)"},
		{"sqrt expression", "\\sqrt{x+1}", "√(x+1)"},

		// Fractions
		{"fraction", "\\frac{a}{b}", "(a)/(b)"},
		{"fraction complex", "\\frac{x+1}{y-1}", "(x+1)/(y-1)"},

		// Text commands
		{"mathrm", "\\mathrm{mod}", "mod"},
		{"text", "\\text{if}", "if"},

		// Complex expressions
		{
			"user example",
			"y^2 = x^3 + 7\\ (\\mathrm{mod}\\ p)",
			"y² = x³ + 7 (mod p)",
		},
		{
			"quadratic formula",
			"x = \\frac{-b \\pm \\sqrt{b^2 - 4ac}}{2a}",
			"x = (-b ± √(b² - 4ac))/(2a)",
		},
		{
			"limit",
			"\\lim_{n \\to \\infty} a_n = L",
			"\\limₙ → ∞ aₙ = L", // \lim stays as-is (not a defined command)
		},
		{
			"sum notation",
			"\\sum_{i=1}^{n} i^2",
			"∑ᵢ₌₁ⁿ i²", // Subscripts for i=1 convert
		},
		{
			"set builder",
			"\\{x \\in \\mathbb{R} : x > 0\\}",
			"{x ∈ R : x > 0}",
		},
		{
			"derivative",
			"\\frac{dy}{dx} = 2x",
			"(dy)/(dx) = 2x",
		},
		{
			"integral bounds",
			"\\int_0^\\infty e^{-x} dx",
			"∫₀^∞ e⁻ˣ dx",
		},

		// Delimiters should be stripped
		{"display math brackets", "\\[x^2\\]", "x²"},
		{"display math dollars", "$$x^2$$", "x²"},
		{"inline math parens", "\\(x^2\\)", "x²"},
		{"inline math dollar", "$x^2$", "x²"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ToUnicode(tt.input)
			if result != tt.expected {
				t.Errorf("ToUnicode(%q) = %q, want %q", tt.input, result, tt.expected)
			}
		})
	}
}

func TestContainsLaTeX(t *testing.T) {
	tests := []struct {
		input    string
		expected bool
	}{
		{"plain text", false},
		{"x = 2", false},
		{"\\[x^2\\]", true},
		{"$$x^2$$", true},
		{"the formula $x^2$ is", true},
		{"price is $5", true}, // Our simple detection triggers - that's OK
		{"\\(x\\)", true},
		{"\\alpha", false}, // Just a command isn't math mode
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result := ContainsLaTeX(tt.input)
			if result != tt.expected {
				t.Errorf("ContainsLaTeX(%q) = %v, want %v", tt.input, result, tt.expected)
			}
		})
	}
}

func TestProcessText(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			"mixed text and math",
			"The equation \\(x^2 + y^2 = r^2\\) describes a circle.",
			"The equation x² + y² = r² describes a circle.",
		},
		{
			"display math",
			"Consider:\n\\[E = mc^2\\]\nThis is famous.",
			"Consider:\nE = mc²\nThis is famous.",
		},
		{
			"multiple inline",
			"If $a > b$ and $b > c$, then $a > c$.",
			"If a > b and b > c, then a > c.",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ProcessText(tt.input)
			if result != tt.expected {
				t.Errorf("ProcessText(%q) = %q, want %q", tt.input, result, tt.expected)
			}
		})
	}
}

// Benchmark to ensure performance is reasonable
func BenchmarkToUnicode(b *testing.B) {
	latex := "\\frac{-b \\pm \\sqrt{b^2 - 4ac}}{2a}"
	for i := 0; i < b.N; i++ {
		ToUnicode(latex)
	}
}
