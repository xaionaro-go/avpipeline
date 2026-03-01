// Package cascadedata provides embedded Haar cascade XML data for common detection tasks.
package cascadedata

import _ "embed"

// FaceFrontalDefault is the OpenCV Haar cascade for frontal face detection.
//
//go:embed haarcascade_frontalface_default.xml
var FaceFrontalDefault []byte

// PlateRussian is the OpenCV Haar cascade for Russian license plate detection.
//
//go:embed haarcascade_russian_plate_number.xml
var PlateRussian []byte
