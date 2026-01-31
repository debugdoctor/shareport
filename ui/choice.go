package ui

import "fmt"

func ClampChoice(input string, max int) int {
	n := 1
	_, _ = fmt.Sscanf(input, "%d", &n)
	if n < 1 {
		return 1
	}
	if n > max {
		return max
	}
	return n
}
