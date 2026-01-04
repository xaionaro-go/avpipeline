//go:build with_cv
// +build with_cv

// abstract.go defines the Abstract interface for Haar cascade processors.

// Package haarcascadeprocessor provides Haar cascade image processing kernels.
package haarcascadeprocessor

import (
	"github.com/xaionaro-go/avpipeline/kernel"
)

type Abstract = kernel.HaarCascadeProcessor
