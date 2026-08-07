package docker

import (
	"context"
	"sort"
	"strconv"
	"time"

	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/api/types/image"
)

// danglingTag is the placeholder the engine reports for an untagged image.
const danglingTag = "<none>:<none>"

// ListImages returns the images matching opts, newest first.
func (e *engine) ListImages(ctx context.Context, opts ListImagesOptions) ([]Image, error) {
	args := filters.NewArgs()
	for _, l := range opts.Label {
		args.Add("label", l)
	}
	if opts.Dangling != nil {
		args.Add("dangling", strconv.FormatBool(*opts.Dangling))
	}

	summaries, err := e.api.ImageList(ctx, image.ListOptions{
		All:     opts.All,
		Filters: args,
	})
	if err != nil {
		return nil, classify("image.list", "image", "", err)
	}

	out := make([]Image, 0, len(summaries))
	for _, s := range summaries {
		out = append(out, toImage(s))
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Created.After(out[j].Created) })
	return out, nil
}

func toImage(s image.Summary) Image {
	repoTags := s.RepoTags
	if repoTags == nil {
		repoTags = []string{}
	}
	repoDigests := s.RepoDigests
	if repoDigests == nil {
		repoDigests = []string{}
	}

	return Image{
		ID:          s.ID,
		ParentID:    s.ParentID,
		RepoTags:    repoTags,
		RepoDigests: repoDigests,
		Created:     time.Unix(s.Created, 0).UTC(),
		Size:        s.Size,
		SharedSize:  s.SharedSize,
		Labels:      nonNilMap(s.Labels),
		Containers:  s.Containers,
		Dangling:    isDangling(repoTags),
	}
}

// isDangling reports an image with no usable repository tag.
func isDangling(repoTags []string) bool {
	if len(repoTags) == 0 {
		return true
	}
	for _, t := range repoTags {
		if t != danglingTag {
			return false
		}
	}
	return true
}
