package webhooks

import (
	"fmt"
	"net/url"

	"github.com/sre-norns/wyrd/pkg/manifest"
)

type WebhookSpec struct {
	Schema string `form:"schema" json:"schema" yaml:"schema" xml:"schema" gorm:"not null;"`
	Host   string `form:"host" json:"host,omitempty" yaml:"host,omitempty" xml:"host" gorm:"not null;"`
	Path   string `form:"path" json:"path,omitempty" yaml:"path,omitempty" xml:"path"`

	// TODO: Record call results?
	// TODO: Enable circuit breaker per-host?
}

func (w WebhookSpec) TargetURL() (*url.URL, error) {
	target, err := url.Parse(fmt.Sprintf("%s://%v", w.Schema, w.Host))
	if err != nil {
		return target, err
	}

	target = target.JoinPath(w.Path)
	return target, err
}

type ResourceDiff struct {
	Kind manifest.Kind
	ID   manifest.ResourceID
}

type EventPayload struct {
	// TODO: Capture who changed the resources
	Principle any `json:"who,omitempty" yaml:"who,omitempty" xml:"who,omitempty"`

	// List of newly created resources
	Created []manifest.ResourceManifest `json:"created,omitempty" yaml:"created,omitempty" xml:"created,omitempty"`
	// List of removed resources
	Deleted []manifest.ResourceManifest `json:"deleted,omitempty" yaml:"deleted,omitempty" xml:"deleted,omitempty"`
	// List of updated resources
	Modified []ResourceDiff `json:"modified,omitempty" yaml:"modified,omitempty" xml:"modified,omitempty"`
}

const (
	KindWebhook manifest.Kind = "Webhook"
)

type Webhook manifest.ResourceModel[WebhookSpec]

func init() {
	manifest.MustRegisterKind(KindWebhook, &WebhookSpec{})
}
