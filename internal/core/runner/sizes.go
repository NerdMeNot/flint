package runner

// TShirtSize maps human-friendly size names to resource profiles.
type TShirtSize string

const (
	SizeXS  TShirtSize = "xs"
	SizeS   TShirtSize = "small"
	SizeM   TShirtSize = "medium"
	SizeL   TShirtSize = "large"
	SizeXL  TShirtSize = "xl"
	Size2XL TShirtSize = "2xl"
)

// tshirtSizes maps size names to CPU and memory defaults.
var tshirtSizes = map[TShirtSize]ResourceProfile{
	SizeXS:  {CPUMillis: 500, MemoryMB: 1024},
	SizeS:   {CPUMillis: 1000, MemoryMB: 2048},
	SizeM:   {CPUMillis: 2000, MemoryMB: 4096},
	SizeL:   {CPUMillis: 4000, MemoryMB: 8192},
	SizeXL:  {CPUMillis: 8000, MemoryMB: 16384},
	Size2XL: {CPUMillis: 16000, MemoryMB: 32768},
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
