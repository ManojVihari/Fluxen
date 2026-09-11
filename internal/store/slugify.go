package store

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	"fluxen/pkg/types"
)

var (
	slugNonAlnum = regexp.MustCompile(`[^a-z0-9]+`)
	slugTrim     = regexp.MustCompile(`^-+|-+$`)
)

// Slugify turns an application name into a URL-safe slug ("Document AI"
// -> "document-ai"). It never fails — an empty or entirely non-alphanumeric
// name simply produces "app".
func Slugify(name string) string {
	s := strings.ToLower(name)
	s = slugNonAlnum.ReplaceAllString(s, "-")
	s = slugTrim.ReplaceAllString(s, "")
	if s == "" {
		return "app"
	}
	return s
}

// UniqueSlug returns a slug for name that doesn't collide with an existing
// application in orgID, appending "-2", "-3", ... as needed.
func (a *Applications) UniqueSlug(ctx context.Context, orgID types.OrgID, name string) (string, error) {
	base := Slugify(name)
	slug := base

	for i := 2; ; i++ {
		exists, err := a.SlugExists(ctx, orgID, slug)
		if err != nil {
			return "", err
		}
		if !exists {
			return slug, nil
		}
		slug = fmt.Sprintf("%s-%d", base, i)
	}
}
