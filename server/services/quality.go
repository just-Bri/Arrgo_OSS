package services

import (
	"regexp"
	"strings"
)

// Quality levels
const (
	Quality4K    = "4k"
	Quality1080p = "1080p"
	Quality720p  = "720p"
	Quality480p  = "480p"
	QualitySD    = "SD"
	QualityUnknown = "Unknown"
)

var qualityMap = map[string]int{
	Quality4K:    4,
	Quality1080p: 3,
	Quality720p:  2,
	Quality480p:  1,
	QualitySD:    0,
	QualityUnknown: -1,
}

func DetectQuality(filename string) string {
	filename = strings.ToLower(filename)
	
	if regexp.MustCompile(`2160p|4k|uhd`).MatchString(filename) {
		return Quality4K
	}
	if regexp.MustCompile(`1080p|fhd`).MatchString(filename) {
		return Quality1080p
	}
	if regexp.MustCompile(`720p|hd`).MatchString(filename) {
		return Quality720p
	}
	if regexp.MustCompile(`480p|576p`).MatchString(filename) {
		return Quality480p
	}
	if regexp.MustCompile(`dvd|sd|xvid|divx`).MatchString(filename) {
		return QualitySD
	}
	
	return QualityUnknown
}

// CompareQuality returns:
// 1 if q1 > q2
// -1 if q1 < q2
// 0 if q1 == q2
func CompareQuality(q1, q2 string) int {
	v1 := qualityMap[q1]
	v2 := qualityMap[q2]
	
	if v1 > v2 {
		return 1
	}
	if v1 < v2 {
		return -1
	}
	return 0
}
