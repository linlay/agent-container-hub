package util

func CloneMap[V any](src map[string]V) map[string]V {
	if len(src) == 0 {
		return nil
	}
	dst := make(map[string]V, len(src))
	for k, v := range src {
		dst[k] = v
	}
	return dst
}
