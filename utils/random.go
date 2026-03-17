package utils

import (
	"crypto/rand"
	"fmt"
	"math/big"
)

func RandomAlphanumeric(n int) string {
	const letters = "0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz"
	ret := make([]byte, n)
	for i := 0; i < n; i++ {
		num, _ := rand.Int(rand.Reader, big.NewInt(int64(len(letters))))
		ret[i] = letters[num.Int64()]
	}
	return string(ret)
}

func RandomNumbers(n int) string {
	const numbers = "0123456789"
	ret := make([]byte, n)
	for i := 0; i < n; i++ {
		num, _ := rand.Int(rand.Reader, big.NewInt(int64(len(numbers))))
		ret[i] = numbers[num.Int64()]
	}
	return string(ret)
}
