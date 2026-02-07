package niconico

import (
	"fmt"
	"testing"
)

func BenchmarkNiconicoSort(b *testing.B) {
	base := make([]string, 1000)
	for i := range base {
		base[i] = fmt.Sprintf("sm%d", 1000-i)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		values := append([]string(nil), base...)
		NiconicoSort(values)
	}
}
