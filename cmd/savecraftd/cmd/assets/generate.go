//go:build ignore

// Generates a placeholder icon.png for the system tray.
package main

import (
	"image"
	"image/color"
	"image/png"
	"os"
)

func main() {
	const size = 64
	img := image.NewRGBA(image.Rect(0, 0, size, size))
	// Savecraft green placeholder
	fill := color.RGBA{R: 0x2e, G: 0xcc, B: 0x71, A: 0xff}
	for x := 0; x < size; x++ {
		for y := 0; y < size; y++ {
			img.Set(x, y, fill)
		}
	}
	f, err := os.Create("icon.png")
	if err != nil {
		panic(err)
	}
	defer f.Close()
	if err := png.Encode(f, img); err != nil {
		panic(err)
	}
}
