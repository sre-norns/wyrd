package manifest

// HLink is a struct to hold semantic web links, representing action that can be performed on response item
type HLink struct {
	Reference    string `form:"ref" json:"ref,omitempty" yaml,omitempty:"ref" xml:"ref"`
	Relationship string `form:"rel" json:"rel,omitempty" yaml:"rel,omitempty" xml:"rel"`
}

// HResponse is a response object, produced by a server that has semantic references
type HResponse struct {
	Links map[string]HLink `form:"_links" json:"_links,omitempty" yaml:"_links,omitempty" xml:"_links"`
}
