package main

const (
	USE_COLORS         = true
	STYLE_DIM          = false
	STYLE_BOLD_HEADERS = true
)

func styleDim(text string) string {
	if !USE_COLORS || !STYLE_DIM {
		return text
	}
	return "\033[2m" + text + "\033[0m"
}

func styleBold(text string) string {
	if !USE_COLORS || !STYLE_BOLD_HEADERS {
		return text
	}
	return "\033[1m" + text + "\033[0m"
}

func label(s string) string {
	return styleBold(s)
}
