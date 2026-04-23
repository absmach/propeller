package sdf

type Document struct {
	Info             Info                      `json:"info"`
	Namespace        map[string]string         `json:"namespace,omitempty"`
	DefaultNamespace string                    `json:"defaultNamespace,omitempty"`
	SdfProperty      map[string]PropertyAfford `json:"sdfProperty,omitempty"`
	SdfAction        map[string]ActionAfford   `json:"sdfAction,omitempty"`
	SdfEvent         map[string]EventAfford    `json:"sdfEvent,omitempty"`
	SdfThing         map[string]ThingAfford    `json:"sdfThing,omitempty"`
	SdfData          map[string]DataAfford     `json:"sdfData,omitempty"`
}

type Info struct {
	Title     string `json:"title"`
	Version   string `json:"version"`
	Copyright string `json:"copyright,omitempty"`
	License   string `json:"license,omitempty"`
}

type DataAfford struct {
	Description          string                `json:"description,omitempty"`
	Type                 string                `json:"type,omitempty"`
	SdfRef               string                `json:"sdfRef,omitempty"`
	Properties           map[string]DataAfford `json:"properties,omitempty"`
	AdditionalProperties *DataAfford           `json:"additionalProperties,omitempty"`
	Items                *DataAfford           `json:"items,omitempty"`
	ReadOnly             bool                  `json:"readOnly,omitempty"`
	Minimum              *float64              `json:"minimum,omitempty"`
	Maximum              *float64              `json:"maximum,omitempty"`
	Enum                 []any                 `json:"enum,omitempty"`
	Required             []string              `json:"required,omitempty"`
}

type PropertyAfford struct {
	DataAfford

	Observable bool `json:"observable,omitempty"`
}

type ActionAfford struct {
	Description   string      `json:"description,omitempty"`
	SdfInputData  *DataAfford `json:"sdfInputData,omitempty"`
	SdfOutputData *DataAfford `json:"sdfOutputData,omitempty"`
}

type EventAfford struct {
	Description   string      `json:"description,omitempty"`
	SdfOutputData *DataAfford `json:"sdfOutputData,omitempty"`
}

type ThingAfford struct {
	Description string                    `json:"description,omitempty"`
	SdfProperty map[string]PropertyAfford `json:"sdfProperty,omitempty"`
	SdfAction   map[string]ActionAfford   `json:"sdfAction,omitempty"`
	SdfEvent    map[string]EventAfford    `json:"sdfEvent,omitempty"`
	SdfData     map[string]DataAfford     `json:"sdfData,omitempty"`
}

type ObjectAfford = ThingAfford
