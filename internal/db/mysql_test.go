package db

import "testing"

func TestPoolSize(t *testing.T) {
	tests := []struct {
		workers int
		want    int
	}{
		{workers: 4, want: 6},
		{workers: 1, want: 3},
		{workers: 0, want: 2},
		{workers: -1, want: 2},
	}

	for _, tt := range tests {
		if got := poolSize(tt.workers); got != tt.want {
			t.Fatalf("poolSize(%d) = %d, want %d", tt.workers, got, tt.want)
		}
	}
}
