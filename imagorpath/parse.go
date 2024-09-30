package imagorpath

import (
	"fmt"
	"net/url"
	"regexp"
	"strconv"
	"strings"
)

var pathRegex = regexp.MustCompile(
	"/*" +
		// params
		"(params/)?" +
		// hash
		"((unsafe/)|([A-Za-z0-9-_=]{8,})/)?" +
		// path
		"(.+)?",
)

var paramsRegex = regexp.MustCompile(
	"/*" +
		// meta
		"(meta/)?" +
		// trim
		"(trim(:(top-left|bottom-right))?(:(\\d+))?/)?" +
		// crop
		"(((0?\\.)?\\d+)x((0?\\.)?\\d+):(([0-1]?\\.)?\\d+)x(([0-1]?\\.)?\\d+)/)?" +
		// fit-in
		"(fit-in/)?" +
		// stretch
		"(stretch/)?" +
		// dimensions
		"((\\-?)(\\d*)x(\\-?)(\\d*)/)?" +
		// paddings
		"((\\d+)x(\\d+)(:(\\d+)x(\\d+))?/)?" +
		// h_align
		"((left|right|center)/)?" +
		// v_align
		"((top|bottom|middle)/)?" +
		// smart
		"(smart/)?" +
		// filters and image
		"(.+)?",
)

// Parse Params struct from imagor endpoint URI
func Parse(path string) Params {
	var p Params
	return Apply(p, path)
}

// c1361ee7-0379-4687-bb4b-60d2bed2550e/-/preview/-/crop/509x574/243,134/-/rotate/270/-/preview/500x500/-/resize/300x/
// 413f8bfd-aae0-4fe5-a584-2128aab093ec/-/crop/545x545/103,0/-/overlay/3ebe2903-f913-4293-9228-2ec723dbb175/100px100p/center/-/resize/300x/
func ApplyUC(p Params, uuid string, isParams bool, path string) Params {
	// Split path into components
	components := strings.Split(path, "/-/")
	// TODO: replace base URL
	p.Image = "https://ucarecdn.com/" + uuid + "/"
	p.Unsafe = true
	p.Params = isParams

	fmt.Println("uuid", p.Image)
	fmt.Println("path", path)

	if len(components) < 2 {
		return p
	}

	fmt.Println("components", components)

	// Process each transformation
	for _, component := range components[1:] {
		parts := strings.SplitN(component, "/", 2)
		operation := parts[0]
		var args string
		if len(parts) > 1 {
			args = strings.TrimRight(parts[1], "/")
		}

		fmt.Println("operation", operation)
		fmt.Println("args", args)
		fmt.Println("")

		switch operation {
		case "preview":
			if args != "" {
				dimensions := strings.Split(args, "x")
				if len(dimensions) == 2 {
					p.FitIn = true
					p.Width, _ = strconv.Atoi(dimensions[0])
					p.Height, _ = strconv.Atoi(dimensions[1])
				}
			}
		case "crop":
			cropArgs := strings.Split(args, "/")
			if len(cropArgs) == 2 {
				dimensions := strings.Split(cropArgs[0], "x")
				offset := strings.Split(cropArgs[1], ",")
				if len(dimensions) == 2 && len(offset) == 2 {
					p.Width, _ = strconv.Atoi(dimensions[0])
					p.Height, _ = strconv.Atoi(dimensions[1])
					x, _ := strconv.Atoi(offset[0])
					y, _ := strconv.Atoi(offset[1])
					p.CropLeft = float64(x)
					p.CropTop = float64(y)
					p.CropRight = float64(x + p.Width)
					p.CropBottom = float64(y + p.Height)
				}
			}
		case "grayscale":
			p.Filters = append(p.Filters, Filter{Name: "grayscale", Args: ""})
		case "sharp":
			// e.g. https://ucarecdn.com/34a7b653-3859-4748-ae10-5b92bc861972/-/crop/2721x1812/907,3997/-/preview/-/rotate/270/-/sharp/20/-/preview/1162x693/-/setfill/ffffff/-/format/jpeg/-/progressive/yes/
			sigma, _ := strconv.Atoi(args)
			p.Filters = append(p.Filters, Filter{Name: "sharpen", Args: strconv.Itoa(sigma / 10)})
		case "rotate":
			angle, _ := strconv.Atoi(args)
			p.Filters = append(p.Filters, Filter{Name: "rotate", Args: strconv.Itoa(angle)})
		case "enhance":
			strength, _ := strconv.Atoi(args)
			p.Filters = append(p.Filters, Filter{Name: "auto_levels", Args: strconv.Itoa(strength)})
		case "resize":
			dimensions := strings.Split(args, "x")
			fmt.Println("dimensions", dimensions)
			if len(dimensions) > 0 {
				if dimensions[0] != "" {
					p.Width, _ = strconv.Atoi(dimensions[0])
				}
				if len(dimensions) > 1 && dimensions[1] != "" {
					p.Height, _ = strconv.Atoi(dimensions[1])
				}
				if p.Width != 0 && p.Height != 0 {
					p.Stretch = true
				} else {
					p.FitIn = true
				}
			}
		case "overlay":
			overlayArgs := strings.Split(args, "/")
			if len(overlayArgs) == 3 {
				overlayUUID := overlayArgs[0]
				size := strings.Split(overlayArgs[1], "x")
				position := overlayArgs[2]

				fmt.Println("size", size)

				var width, height string
				if len(size) == 2 {
					width = strings.TrimSuffix(size[0], "p")
					height = strings.TrimSuffix(size[1], "p")
				}

				fmt.Println("width", width)
				fmt.Println("height", height)

				overlayUrl := "https://ucarecdn.com/" + overlayUUID + "/"
				watermarkFilter := fmt.Sprintf("%s,%s,%s,0,%s,%s,force", overlayUrl, position, position, width, height)
				p.Filters = append(p.Filters, Filter{Name: "watermark", Args: watermarkFilter})
			}
		}
	}

	// If there is a rotate filter with an angle of 90 or 270, switch the width and height to imitate Uploadcare's behavior
	for _, filter := range p.Filters {
		if filter.Name == "rotate" {
			angle, _ := strconv.Atoi(filter.Args)
			if angle == 90 || angle == 270 {
				p.Width, p.Height = p.Height, p.Width
			}
		}
	}

	return p
}

// Apply Params struct from imagor endpoint URI on top of existing Params
func Apply(p Params, path string) Params {

	isUCFormat, uuid, isParams, _ := IsUCFormat(path)
	if isUCFormat {
		return ApplyUC(p, uuid, isParams, path)
	}

	match := pathRegex.FindStringSubmatch(breaksCleaner.Replace(path))
	if len(match) < 6 {
		return p
	}
	index := 1
	if match[index] != "" {
		p.Params = true
	}
	index++
	if match[index+1] == "unsafe/" {
		p.Unsafe = true
	} else if len(match[index+2]) > 8 {
		p.Hash = match[index+2]
	}
	index += 3
	p.Path = match[index]

	match = paramsRegex.FindStringSubmatch(p.Path)
	if len(match) == 0 {
		return p
	}
	index = 1
	if match[index] != "" {
		p.Meta = true
	}
	index++
	if match[index] != "" {
		p.Trim = true
		p.TrimBy = TrimByTopLeft
		if s := match[index+2]; s != "" {
			p.TrimBy = s
		}
		p.TrimTolerance, _ = strconv.Atoi(match[index+4])
	}
	index += 5
	if match[index] != "" {
		p.CropLeft, _ = strconv.ParseFloat(match[index+1], 64)
		p.CropTop, _ = strconv.ParseFloat(match[index+3], 64)
		p.CropRight, _ = strconv.ParseFloat(match[index+5], 64)
		p.CropBottom, _ = strconv.ParseFloat(match[index+7], 64)
	}
	index += 9
	if match[index] != "" {
		p.FitIn = true
	}
	index++
	if match[index] != "" {
		p.Stretch = true
	}
	index++
	if match[index] != "" {
		p.HFlip = match[index+1] != ""
		p.Width, _ = strconv.Atoi(match[index+2])
		p.VFlip = match[index+3] != ""
		p.Height, _ = strconv.Atoi(match[index+4])
	}
	index += 5
	if match[index] != "" {
		p.PaddingLeft, _ = strconv.Atoi(match[index+1])
		p.PaddingTop, _ = strconv.Atoi(match[index+2])
		if match[index+3] != "" {
			p.PaddingRight, _ = strconv.Atoi(match[index+4])
			p.PaddingBottom, _ = strconv.Atoi(match[index+5])
		} else {
			p.PaddingRight = p.PaddingLeft
			p.PaddingBottom = p.PaddingTop
		}
	}
	index += 6
	if match[index] != "" {
		p.HAlign = match[index+1]
	}
	index += 2
	if match[index] != "" {
		p.VAlign = match[index+1]
	}
	index += 2
	if match[index] != "" {
		p.Smart = true
	}
	index++
	if match[index] != "" {
		filters, img := parseFilters(match[index])
		p.Filters = append(p.Filters, filters...)
		if img != "" {
			p.Image = img
			if u, err := url.QueryUnescape(img); err == nil {
				p.Image = u
			}
		}
	}
	return p
}

func parseFilters(str string) (filters []Filter, path string) {
	if strings.HasPrefix(str, "filters:") {
		str = str[8:]
		var s strings.Builder
		var depth int
		var name, args string
		for idx, ch := range str {
			switch ch {
			case '(':
				if depth == 0 {
					name = s.String()
					s.Reset()
				} else {
					s.WriteRune(ch)
				}
				depth++
			case ')':
				depth--
				if depth == 0 {
					args = s.String()
					s.Reset()
				} else {
					s.WriteRune(ch)
				}
			case '/':
				if depth == 0 {
					path = str[idx+1:]
				} else {
					s.WriteRune(ch)
				}
			case ':':
				if depth == 0 {
					filters = append(filters, Filter{
						Name: name, Args: args,
					})
					name = ""
					args = ""
					s.Reset()
				} else {
					s.WriteRune(ch)
				}
			default:
				s.WriteRune(ch)
			}
			if path != "" {
				break
			}
		}
		if name != "" {
			filters = append(filters, Filter{
				Name: name, Args: args,
			})
		}
	} else {
		path = str
	}
	return
}

// Helper function to check if a string is a valid UUID
func isUUID(s string) bool {
	// UUID format: 8-4-4-4-12 (32 characters + 4 hyphens)
	if len(s) != 36 {
		return false
	}

	// Check for correct hyphen positions and valid hexadecimal characters
	segments := strings.Split(s, "-")
	if len(segments) != 5 || len(segments[0]) != 8 || len(segments[1]) != 4 || len(segments[2]) != 4 || len(segments[3]) != 4 || len(segments[4]) != 12 {
		return false
	}

	return true
}

// IsUCFormat checks if the given path is in the Uploadcare format
func IsUCFormat(path string) (bool, string, bool, string) {
	segments := strings.Split(strings.Trim(path, "/"), "/")

	var uuid string
	isUCFormat := false
	isUCParams := false
	ucPath := ""
	if len(segments) > 0 {
		if isUUID(segments[0]) {
			isUCFormat = true
			uuid = segments[0]
			ucPath = strings.Join(segments[1:], "/")
		}
		if segments[0] == "params" && len(segments) > 1 && isUUID(segments[1]) {
			isUCFormat = true
			uuid = segments[1]
			isUCParams = true
			ucPath = strings.Join(segments[2:], "/")
		}
	}
	return isUCFormat, uuid, isUCParams, ucPath
}
