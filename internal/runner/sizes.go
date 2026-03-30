package runner

import "k8s.io/apimachinery/pkg/api/resource"

// TShirtSize maps human-friendly size names to resource profiles.
type TShirtSize string

const (
	SizeXS   TShirtSize = "xs"
	SizeS    TShirtSize = "small"
	SizeM    TShirtSize = "medium"
	SizeL    TShirtSize = "large"
	SizeXL   TShirtSize = "xl"
	Size2XL  TShirtSize = "2xl"
)

// tshirtSizes maps size names to CPU and memory defaults.
var tshirtSizes = map[TShirtSize]ResourceProfile{
	SizeXS:  {CPU: resource.MustParse("500m"), Memory: resource.MustParse("1Gi")},
	SizeS:   {CPU: resource.MustParse("1"), Memory: resource.MustParse("2Gi")},
	SizeM:   {CPU: resource.MustParse("2"), Memory: resource.MustParse("4Gi")},
	SizeL:   {CPU: resource.MustParse("4"), Memory: resource.MustParse("8Gi")},
	SizeXL:  {CPU: resource.MustParse("8"), Memory: resource.MustParse("16Gi")},
	Size2XL: {CPU: resource.MustParse("16"), Memory: resource.MustParse("32Gi")},
}

// ResourcesForSize returns the resource profile for a t-shirt size.
// Returns false if the size is not recognized.
func ResourcesForSize(size TShirtSize) (ResourceProfile, bool) {
	p, ok := tshirtSizes[size]
	return p, ok
}

// ValidSize returns true if the size is a recognized t-shirt size.
func ValidSize(size TShirtSize) bool {
	_, ok := tshirtSizes[size]
	return ok
}

// AllSizes returns all t-shirt sizes in ascending order.
func AllSizes() []TShirtSize {
	return []TShirtSize{SizeXS, SizeS, SizeM, SizeL, SizeXL, Size2XL}
}
