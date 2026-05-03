//go:build with_cv
// +build with_cv

package haarcascadeprocessor

import (
	"context"
	"image"
	"testing"

	testifyassert "github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gocv.io/x/gocv"
)

func newTestMat(t *testing.T, w, h int, value uint8) gocv.Mat {
	t.Helper()
	mat := gocv.NewMatWithSizeFromScalar(gocv.NewScalar(float64(value), float64(value), float64(value), 0), h, w, gocv.MatTypeCV8UC3)
	require.False(t, mat.Empty())
	return mat
}

func TestGaussianBlur_Process(t *testing.T) {
	ctx := context.Background()
	mat := newTestMat(t, 100, 100, 200)
	defer mat.Close()

	b := NewGaussianBlur(5)
	rects := []image.Rectangle{
		image.Rect(10, 10, 50, 50),
	}
	err := b.Process(ctx, &mat, rects)
	require.NoError(t, err)
	// The mat should still be the same size.
	testifyassert.Equal(t, 100, mat.Cols())
	testifyassert.Equal(t, 100, mat.Rows())
}

func TestGaussianBlur_ZeroRadius(t *testing.T) {
	ctx := context.Background()
	mat := newTestMat(t, 100, 100, 128)
	defer mat.Close()

	b := NewGaussianBlur(0)
	rects := []image.Rectangle{
		image.Rect(10, 10, 50, 50),
	}
	err := b.Process(ctx, &mat, rects)
	require.NoError(t, err)
}

func TestGaussianBlur_ClampToMatBounds(t *testing.T) {
	ctx := context.Background()
	mat := newTestMat(t, 50, 50, 128)
	defer mat.Close()

	b := NewGaussianBlur(3)
	// Rect extends beyond mat bounds.
	rects := []image.Rectangle{
		image.Rect(30, 30, 100, 100),
	}
	err := b.Process(ctx, &mat, rects)
	require.NoError(t, err)
}

func TestGaussianBlur_EmptyRects(t *testing.T) {
	ctx := context.Background()
	mat := newTestMat(t, 100, 100, 128)
	defer mat.Close()

	b := NewGaussianBlur(5)
	err := b.Process(ctx, &mat, nil)
	require.NoError(t, err)
}

func TestGaussianBlur_String(t *testing.T) {
	b := NewGaussianBlur(10)
	testifyassert.Contains(t, b.String(), "GaussianBlur")
}

func TestGaussianBlur_RectOutsideMat(t *testing.T) {
	ctx := context.Background()
	mat := newTestMat(t, 50, 50, 128)
	defer mat.Close()

	b := NewGaussianBlur(3)
	// Rect completely outside mat bounds.
	rects := []image.Rectangle{
		image.Rect(100, 100, 200, 200),
	}
	err := b.Process(ctx, &mat, rects)
	require.NoError(t, err)
}

func TestPixelate_Process(t *testing.T) {
	ctx := context.Background()
	mat := newTestMat(t, 100, 100, 200)
	defer mat.Close()

	p := NewPixelate(10)
	rects := []image.Rectangle{
		image.Rect(10, 10, 50, 50),
	}
	err := p.Process(ctx, &mat, rects)
	require.NoError(t, err)
	testifyassert.Equal(t, 100, mat.Cols())
	testifyassert.Equal(t, 100, mat.Rows())
}

func TestPixelate_BlockSizeOne(t *testing.T) {
	ctx := context.Background()
	mat := newTestMat(t, 100, 100, 128)
	defer mat.Close()

	p := NewPixelate(1)
	rects := []image.Rectangle{
		image.Rect(10, 10, 50, 50),
	}
	err := p.Process(ctx, &mat, rects)
	require.NoError(t, err)
}

func TestPixelate_EmptyRects(t *testing.T) {
	ctx := context.Background()
	mat := newTestMat(t, 100, 100, 128)
	defer mat.Close()

	p := NewPixelate(10)
	err := p.Process(ctx, &mat, nil)
	require.NoError(t, err)
}

func TestPixelate_String(t *testing.T) {
	p := NewPixelate(8)
	testifyassert.Contains(t, p.String(), "Pixelate")
	testifyassert.Contains(t, p.String(), "8")
}

func TestPixelate_ClampToMatBounds(t *testing.T) {
	ctx := context.Background()
	mat := newTestMat(t, 50, 50, 128)
	defer mat.Close()

	p := NewPixelate(5)
	rects := []image.Rectangle{
		image.Rect(30, 30, 100, 100),
	}
	err := p.Process(ctx, &mat, rects)
	require.NoError(t, err)
}
