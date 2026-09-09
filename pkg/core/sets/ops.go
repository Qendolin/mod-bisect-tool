// Package sets provides utility functions for common operations on sets and slices.
// Sets are represented as map[string]struct{} for efficient lookups.
package sets

import "sort"

// TODO: Remove / Move GetSplitIndex and Split (should be internal to imcs)

// GetSplitIndex calculates the index at which to split a slice of a given length
// into two halves. The first half will be larger if the length is odd.
func GetSplitIndex(length int) int {
	return (length + 1) / 2
}

// Split divides a slice into two approximately equal halves.
func Split(slice OrderedSet) (OrderedSet, OrderedSet) {
	if len(slice) == 0 {
		return OrderedSet{}, OrderedSet{}
	}
	mid := GetSplitIndex(len(slice))
	return slice[:mid], slice[mid:]
}

// Union returns a new set containing all elements present in either set a or set b.
func Union(a, b Set) Set {
	result := make(Set, len(a)+len(b))
	for k := range a {
		result[k] = struct{}{}
	}
	for k := range b {
		result[k] = struct{}{}
	}
	return result
}

// AddInPlace adds all elements from b to a and returns the result.
func AddInPlace(a, b Set) Set {
	for k := range b {
		a[k] = struct{}{}
	}
	return a
}

// Intersection returns a new set containing only the elements present in both set a and set b.
func Intersection(a, b Set) Set {
	// For efficiency, iterate over the smaller set.
	if len(a) > len(b) {
		a, b = b, a // Ensure 'a' is the smaller set
	}

	result := make(Set)
	for k := range a {
		if _, found := b[k]; found {
			result[k] = struct{}{}
		}
	}
	return result
}

// Subtract returns a new set containing elements from set a that are not present in set b.
func Subtract(a, b Set) Set {
	result := make(Set)
	for k := range a {
		if _, found := b[k]; !found {
			result[k] = struct{}{}
		}
	}
	return result
}

// Subtract removes all elements in b from a and returns the result.
func SubtractInPlace(a, b Set) Set {
	for k := range b {
		delete(a, k)
	}
	return a
}

// Equal checks if two sets contain the exact same elements.
func Equal(a, b Set) bool {
	if len(a) != len(b) {
		return false
	}
	for k := range a {
		if _, found := b[k]; !found {
			return false
		}
	}
	return true
}

// MakeSet converts a slice of strings into a Set for efficient lookups.
// Duplicates in the slice are removed.
func MakeSet(slice []string) Set {
	set := make(Set, len(slice))
	for _, item := range slice {
		set[item] = struct{}{}
	}
	return set
}

// MakeSlice converts a Set into a new, sorted slice of strings (an OrderedSet).
func MakeSlice(set Set) OrderedSet {
	slice := make(OrderedSet, 0, len(set))
	for k := range set {
		slice = append(slice, k)
	}
	sort.Strings(slice)
	return slice
}

func NewSet(items ...string) Set {
	set := make(Set, len(items))
	for _, item := range items {
		set[item] = struct{}{}
	}
	return set
}

// SubtractSlices returns a new slice containing elements from the 'a' slice
// that are not present in the 'b' slice. The order of elements in 'a' is preserved.
func SubtractSlices(a []string, b []string) []string {
	removeSet := MakeSet(b)
	result := make([]string, 0, len(a))
	for _, item := range a {
		if _, found := removeSet[item]; !found {
			result = append(result, item)
		}
	}
	return result
}

// Copy returns a new set containing all elements from the original set.
func Copy(original Set) Set {
	newSet := make(Set, len(original))
	for k := range original {
		newSet[k] = struct{}{}
	}
	return newSet
}
