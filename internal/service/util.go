package service

import "strconv"

// strconvItoa wraps strconv.Itoa to keep documents.go imports minimal.
func strconvItoa(i int) string {
	return strconv.Itoa(i)
}
