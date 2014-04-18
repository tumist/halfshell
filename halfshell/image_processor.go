// Copyright (c) 2014 Oyster
//
// Permission is hereby granted, free of charge, to any person obtaining a copy
// of this software and associated documentation files (the "Software"), to deal
// in the Software without restriction, including without limitation the rights
// to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
// copies of the Software, and to permit persons to whom the Software is
// furnished to do so, subject to the following conditions:
//
// The above copyright notice and this permission notice shall be included in
// all copies or substantial portions of the Software.
//
// THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
// IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
// FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
// AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
// LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
// OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN
// THE SOFTWARE.

package halfshell

import (
	"fmt"
	"github.com/rafikk/imagick/imagick"
	"math"
	"strings"
)

// ImageProcessor is the public interface for the image processor. It exposes a
// single method to process an image with options.
type ImageProcessor interface {
	ProcessImage(*Image, *ImageProcessorOptions) *Image
}

// ImageProcessorOptions specify the request parameters for the processing
// operation.
type ImageProcessorOptions struct {
	Dimensions ImageDimensions
	BlurRadius float64
	GrayScale  bool
	Crop       *ImageProcessorCropOption
}

type ImageProcessorCropOption struct {
    X float64
    Y float64
}

type imageProcessor struct {
	Config *ProcessorConfig
	Logger *Logger
}

// Creates a new ImageProcessor instance using configuration settings.
func NewImageProcessorWithConfig(config *ProcessorConfig) ImageProcessor {
	return &imageProcessor{
		Config: config,
		Logger: NewLogger("image_processor.%s", config.Name),
	}
}

// The public method for processing an image. The method receives an original
// image and options and returns the processed image.
func (ip *imageProcessor) ProcessImage(image *Image, request *ImageProcessorOptions) *Image {
	processedImage := Image{}
	wand := imagick.NewMagickWand()
	defer wand.Destroy()

	wand.ReadImageBlob(image.Bytes)

	err, cropModified := ip.cropWand(wand, request)
	if err != nil {
		ip.Logger.Warn("Error cropping image: %s", err)
		return nil
	}

	err, scaleModified := ip.scaleWand(wand, request)
	if err != nil {
		ip.Logger.Warn("Error scaling image: %s", err)
		return nil
	}

	err, blurModified := ip.blurWand(wand, request)
	if err != nil {
		ip.Logger.Warn("Error blurring image: %s", err)
		return nil
	}

	err, grayscaleModified := ip.grayscaleWand(wand, request)
	if err != nil {
		ip.Logger.Warn("Error grayscaling image: %s", err)
		return nil
	}

	if !cropModified && !scaleModified && !blurModified && !grayscaleModified {
		processedImage.Bytes = image.Bytes
	} else {
		processedImage.Bytes = wand.GetImageBlob()
	}

	processedImage.MimeType = fmt.Sprintf("image/%s", strings.ToLower(wand.GetImageFormat()))

	return &processedImage
}

func (ip *imageProcessor) cropWand(wand *imagick.MagickWand, request *ImageProcessorOptions) (err error, modified bool) {
	if request.Crop == nil {
		return nil, false
	}
	currentDimensions := ImageDimensions{uint64(wand.GetImageWidth()), uint64(wand.GetImageHeight())}

	var cropWidth, cropHeight uint64
	var cropLeft, cropTop int // crop offsets
	if currentDimensions.AspectRatio() > request.Dimensions.AspectRatio() {
		// Image is wider than requested dimensions so we'll
		// crop the width to match requested aspect.
		cropWidth = ip.getAspectScaledWidth(request.Dimensions.AspectRatio(), currentDimensions.Height)
		cropHeight = currentDimensions.Height
		// Shift crop frame from the left
		cropLeft = int(math.Floor(0.5 + (float64(currentDimensions.Width - cropWidth) * request.Crop.X)))
	} else {
		cropWidth = currentDimensions.Width
		cropHeight = ip.getAspectScaledHeight(request.Dimensions.AspectRatio(), currentDimensions.Width)
		cropTop = int(math.Floor(0.5 + (float64(currentDimensions.Height - cropHeight) * request.Crop.Y)))
	}

	if err = wand.CropImage(uint(cropWidth), uint(cropHeight), cropLeft, cropTop); err != nil {
		ip.Logger.Warn("ImageMagick error cropping image: %s", err)
		return err, true
	}
	return nil, true
}

func (ip *imageProcessor) scaleWand(wand *imagick.MagickWand, request *ImageProcessorOptions) (err error, modified bool) {
	currentDimensions := ImageDimensions{uint64(wand.GetImageWidth()), uint64(wand.GetImageHeight())}
	newDimensions := ip.getScaledDimensions(currentDimensions, request)

	if newDimensions == currentDimensions {
		return nil, false
	}

	if err = wand.ResizeImage(uint(newDimensions.Width), uint(newDimensions.Height), imagick.FILTER_LANCZOS, 1); err != nil {
		ip.Logger.Warn("ImageMagick error resizing image: %s", err)
		return err, true
	}

	if err = wand.SetImageInterpolateMethod(imagick.INTERPOLATE_PIXEL_BICUBIC); err != nil {
		ip.Logger.Warn("ImageMagick error setting interpoliation method: %s", err)
		return err, true
	}

	if err = wand.StripImage(); err != nil {
		ip.Logger.Warn("ImageMagick error stripping image routes and metadata")
		return err, true
	}

	if "JPEG" == wand.GetImageFormat() {
		if err = wand.SetImageInterlaceScheme(imagick.INTERLACE_PLANE); err != nil {
			ip.Logger.Warn("ImageMagick error setting the image interlace scheme")
			return err, true
		}

		if err = wand.SetImageCompression(imagick.COMPRESSION_JPEG); err != nil {
			ip.Logger.Warn("ImageMagick error setting the image compression type")
			return err, true
		}

		if err = wand.SetImageCompressionQuality(uint(ip.Config.ImageCompressionQuality)); err != nil {
			ip.Logger.Warn("sImageMagick error setting compression quality: %s", err)
			return err, true
		}
	}

	return nil, true
}

func (ip *imageProcessor) blurWand(wand *imagick.MagickWand, request *ImageProcessorOptions) (err error, modified bool) {
	if request.BlurRadius != 0 {
		blurRadius := float64(wand.GetImageWidth()) * request.BlurRadius * ip.Config.MaxBlurRadiusPercentage
		if err = wand.GaussianBlurImage(blurRadius, blurRadius); err != nil {
			ip.Logger.Warn("ImageMagick error setting blur radius: %s", err)
		}
		return err, true
	}
	return nil, false
}

func (ip *imageProcessor) grayscaleWand(wand *imagick.MagickWand, request *ImageProcessorOptions) (err error, modified bool) {
	if !ip.Config.GrayscaleDisabled && (ip.Config.GrayscaleByDefault || request.GrayScale) {
		if err = wand.TransformImageColorspace(imagick.COLORSPACE_GRAY); err != nil {
			ip.Logger.Warn("ImageMagick error grayscaling image: %s", err)
		}
		return err, true
	}
	return nil, false
}

func (ip *imageProcessor) getScaledDimensions(currentDimensions ImageDimensions, request *ImageProcessorOptions) ImageDimensions {
	requestDimensions := request.Dimensions
	if requestDimensions.Width == 0 && requestDimensions.Height == 0 {
		requestDimensions = ImageDimensions{Width: ip.Config.DefaultImageWidth, Height: ip.Config.DefaultImageHeight}
	}

	dimensions := ip.scaleToRequestedDimensions(currentDimensions, requestDimensions, request)
	return ip.clampDimensionsToMaxima(dimensions, request)
}

func (ip *imageProcessor) scaleToRequestedDimensions(currentDimensions, requestedDimensions ImageDimensions, request *ImageProcessorOptions) ImageDimensions {
	imageAspectRatio := currentDimensions.AspectRatio()
	if requestedDimensions.Width > 0 && requestedDimensions.Height > 0 {
		requestedAspectRatio := requestedDimensions.AspectRatio()
		ip.Logger.Info("Requested image ratio %f, image ratio %f, %v", requestedAspectRatio, imageAspectRatio, ip.Config.MaintainAspectRatio)

		if !ip.Config.MaintainAspectRatio {
			// If we're not asked to maintain the aspect ratio, give them what they want
			return requestedDimensions
		}

		if requestedAspectRatio > imageAspectRatio {
			// The requested aspect ratio is wider than the image's natural ratio.
			// Thus means the height is the restraining dimension, so unset the
			// width and let the height determine the dimensions.
			return ip.scaleToRequestedDimensions(currentDimensions, ImageDimensions{0, requestedDimensions.Height}, request)
		} else if requestedAspectRatio < imageAspectRatio {
			return ip.scaleToRequestedDimensions(currentDimensions, ImageDimensions{requestedDimensions.Width, 0}, request)
		} else {
			return requestedDimensions
		}
	}

	if requestedDimensions.Width > 0 {
		return ImageDimensions{requestedDimensions.Width, ip.getAspectScaledHeight(imageAspectRatio, requestedDimensions.Width)}
	}

	if requestedDimensions.Height > 0 {
		return ImageDimensions{ip.getAspectScaledWidth(imageAspectRatio, requestedDimensions.Height), requestedDimensions.Height}
	}

	return currentDimensions
}

func (ip *imageProcessor) clampDimensionsToMaxima(dimensions ImageDimensions, request *ImageProcessorOptions) ImageDimensions {
	if ip.Config.MaxImageWidth > 0 && dimensions.Width > ip.Config.MaxImageWidth {
		scaledHeight := ip.getAspectScaledHeight(dimensions.AspectRatio(), ip.Config.MaxImageWidth)
		return ip.clampDimensionsToMaxima(ImageDimensions{ip.Config.MaxImageWidth, scaledHeight}, request)
	}

	if ip.Config.MaxImageHeight > 0 && dimensions.Height > ip.Config.MaxImageHeight {
		scaledWidth := ip.getAspectScaledWidth(dimensions.AspectRatio(), ip.Config.MaxImageHeight)
		return ip.clampDimensionsToMaxima(ImageDimensions{scaledWidth, ip.Config.MaxImageHeight}, request)
	}

	return dimensions
}

func (ip *imageProcessor) getAspectScaledHeight(aspectRatio float64, width uint64) uint64 {
	return uint64(math.Floor((float64(width) / aspectRatio) + 0.5))
}

func (ip *imageProcessor) getAspectScaledWidth(aspectRatio float64, height uint64) uint64 {
	return uint64(math.Floor((float64(height) * aspectRatio) + 0.5))
}
