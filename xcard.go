package vcard

import (
	"encoding/xml"
	"errors"
	"sort"
	"strings"
)

// RFC 6351 (xCard: vCard XML Representation) support.
//
// The types and functions in this file and xcard_text.go/xcard_xml.go
// implement optional, bidirectional conversion between a restricted
// subset of vCard 4 plain text (RFC 6350) and the xCard XML representation
// (RFC 6351). The existing text codec, Card, Field, Params, the name/address
// helpers and ToV4 are unchanged.

const (
	// XCardNamespace is the XML namespace defined by RFC 6351 section 8.1.
	XCardNamespace = "urn:ietf:params:xml:ns:vcard-4.0"

	// XCardMIMEType is the media type defined by RFC 6351 section 8.2.
	XCardMIMEType = "application/vcard+xml"
)

// Value type tokens. These identify the vCard 4 / xCard value types.
const (
	ValueText          = "text"
	ValueTextList      = "text-list"
	ValueURI           = "uri"
	ValueBoolean       = "boolean"
	ValueInteger       = "integer"
	ValueFloat         = "float"
	ValueDate          = "date"
	ValueTime          = "time"
	ValueDateTime      = "date-time"
	ValueDateAndOrTime = "date-and-or-time"
	ValueTimestamp     = "timestamp"
	ValueUTCOffset     = "utc-offset"
	ValueLanguageTag   = "language-tag"
	ValueUnknown       = "unknown"

	// Structured value tokens. They only appear as Value.Type for the N,
	// ADR, GENDER, CLIENTPIDMAP and ORG properties.
	ValueStructuredN            = "n"
	ValueStructuredADR          = "adr"
	ValueStructuredGender       = "gender"
	ValueStructuredClientPIDMap = "clientpidmap"
	ValueStructuredOrg          = "org"

	// ValueXML is the value of an XML property (RFC 6350 section 6.1.5),
	// whose content is foreign-namespaced XML embedded directly in xCard.
	ValueXML = "xml"
)

// A Contact is a single vCard object in a lossless, order-preserving
// representation. Unlike Card, property values keep their structure
// (repeated components, list values, typed scalars and embedded XML) so
// that a vCard 4 text <-> xCard round trip preserves contact data.
//
// Contact values are not modified by codec operations: decoders allocate
// new Contacts and encoders treat their argument as read-only.
type Contact struct {
	Properties []*Property
}

// NewContact returns an empty Contact.
func NewContact() *Contact {
	return &Contact{}
}

// Add appends a property to the contact.
func (c *Contact) Add(p *Property) {
	c.Properties = append(c.Properties, p)
}

// PropertiesByName returns all properties with the given (case-insensitive)
// name. The returned slice shares the contact's property pointers.
func (c *Contact) PropertiesByName(name string) []*Property {
	var out []*Property
	for _, p := range c.Properties {
		if strings.EqualFold(p.Name, name) {
			out = append(out, p)
		}
	}
	return out
}

// A Property is one vCard property.
type Property struct {
	// Group is the property group name (the "item1" in "item1.TEL"), or
	// empty.
	Group string
	// Name is the property name in upper case, e.g. "FN" or "X-FOO". For
	// embedded foreign XML it is "XML".
	Name string
	// Params are the property parameters, in order.
	Params []Parameter
	// Value is the property value.
	Value Value
}

// A Parameter is one vCard property parameter. A single parameter name may
// carry several values (e.g. TYPE=work,home).
type Parameter struct {
	// Name is the parameter name, e.g. "TYPE" or "X-FOO".
	Name string
	// Type is the parameter value type: ValueText, ValueInteger,
	// ValueURI, ValueLanguageTag or ValueUnknown.
	Type string
	// Values holds the parameter values.
	Values []string
}

// The Value of a property. Exactly one representation is meaningful
// depending on Type:
//
//   - scalar types (text, uri, boolean, ...): Data holds the content;
//   - ValueTextList: List holds the values;
//   - structured types (n, adr, ...): Parts holds the components;
//   - ValueXML: XML holds the embedded foreign-namespaced element.
type Value struct {
	Type  string
	Data  string
	List  []string
	Parts []Component
	XML   *XMLNode
}

// A Component is one component of a structured value. Name is the xCard
// element name ("surname", "pobox", "sex", "sourceid", ...). Type gives the
// type of each entry in Values ("text", "integer" or "uri"). A component
// with zero values is an empty component.
type Component struct {
	Name   string
	Type   string
	Values []string
}

// An XMLNode is a foreign-namespaced XML element embedded through the vCard
// XML property. Expanded names (namespace URI and local name), attributes
// and content are preserved.
type XMLNode struct {
	Name xml.Name
	Attr []xml.Attr
	Kids []XMLChild
}

// An XMLChild is either an element child (Node != nil) or character data
// (Node == nil, Text set).
type XMLChild struct {
	Text string
	Node *XMLNode
}

// NewXMLNode creates an XMLNode with the given namespace URI, local name and
// attributes.
func NewXMLNode(namespace, local string, attr ...xml.Attr) *XMLNode {
	return &XMLNode{Name: xml.Name{Space: namespace, Local: local}, Attr: attr}
}

// AppendText appends character data to n.
func (n *XMLNode) AppendText(s string) *XMLNode {
	n.Kids = append(n.Kids, XMLChild{Text: s})
	return n
}

// AppendElement appends an element child to n.
func (n *XMLNode) AppendElement(child *XMLNode) *XMLNode {
	n.Kids = append(n.Kids, XMLChild{Node: child})
	return n
}

// ScalarValue builds a scalar Value of the given type.
func ScalarValue(typ, data string) Value {
	return Value{Type: typ, Data: data}
}

// TextValue builds a text Value.
func TextValue(text string) Value {
	return Value{Type: ValueText, Data: text}
}

// TextListValue builds a multi-valued text Value.
func TextListValue(items []string) Value {
	return Value{Type: ValueTextList, List: append([]string(nil), items...)}
}

// StructuredValue builds a structured Value (N, ADR, GENDER, CLIENTPIDMAP or
// ORG).
func StructuredValue(typ string, parts []Component) Value {
	return Value{Type: typ, Parts: parts}
}

// ForeignValue builds a Value carrying an embedded XML element.
func ForeignValue(node *XMLNode) Value {
	return Value{Type: ValueXML, XML: node}
}

// TextParameter builds a single- or multi-valued text parameter.
func TextParameter(name string, values ...string) Parameter {
	return Parameter{Name: name, Type: ValueText, Values: append([]string(nil), values...)}
}

// ScalarParameter builds a parameter with a non-text scalar type
// (ValueInteger, ValueURI or ValueLanguageTag).
func ScalarParameter(name, typ, value string) Parameter {
	return Parameter{Name: name, Type: typ, Values: []string{value}}
}

// NewProperty creates a property with the given group, name and parameters.
func NewProperty(group, name string, params []Parameter, value Value) *Property {
	return &Property{Group: group, Name: name, Params: params, Value: value}
}

// Component is a small helper to construct structured components.
func NewComponent(name, typ string, values ...string) Component {
	return Component{Name: name, Type: typ, Values: append([]string(nil), values...)}
}

// ContactFromCard converts a Card built with the public Card/Field/Params
// API to a lossless Contact. The conversion is best-effort for values that
// went through the legacy text parser: Field.Value cannot distinguish
// escaped commas from list separators in list properties such as
// CATEGORIES and NICKNAME. Values constructed programmatically or read with
// NewVCard4Decoder are converted losslessly.
func ContactFromCard(card Card) (*Contact, error) {
	if card == nil {
		return nil, errors.New("vcard: nil card")
	}

	c := NewContact()

	var keys []string
	for k := range card {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	for _, k := range keys {
		if strings.EqualFold(k, FieldVersion) {
			continue
		}
		for _, f := range card[k] {
			p, err := propertyFromField(k, f)
			if err != nil {
				return nil, err
			}
			c.Add(p)
		}
	}
	return c, nil
}

func propertyFromField(key string, f *Field) (*Property, error) {
	name := strings.ToUpper(key)
	if f == nil {
		f = &Field{}
	}

	p := &Property{Group: f.Group, Name: name}

	explicitValue := ""
	var paramKeys []string
	for k := range f.Params {
		paramKeys = append(paramKeys, k)
	}
	sort.Strings(paramKeys)
	for _, pk := range paramKeys {
		if strings.EqualFold(pk, ParamValue) {
			if len(f.Params[pk]) > 0 {
				explicitValue = strings.ToLower(f.Params[pk][0])
			}
			continue
		}
		p.Params = append(p.Params, Parameter{
			Name:   strings.ToUpper(pk),
			Type:   parameterTypeHint(name, strings.ToUpper(pk), f.Params[pk]),
			Values: append([]string(nil), f.Params[pk]...),
		})
	}

	typ, known := defaultValueType(name)
	if explicitValue != "" {
		if !isScalarValueType(explicitValue) {
			return nil, errors.New("vcard: unknown VALUE type: " + explicitValue)
		}
		typ = explicitValue
	} else if !known {
		if strings.EqualFold(name, FieldXML) {
			typ = ValueXML
		} else {
			typ = ValueUnknown
		}
	}

	v, err := valueFromLegacyField(typ, f.Value)
	if err != nil {
		return nil, err
	}
	p.Value = v
	return p, nil
}

// valueFromLegacyField interprets an already-unescaped Field.Value.
func valueFromLegacyField(typ, raw string) (Value, error) {
	switch typ {
	case ValueTextList:
		return Value{Type: ValueTextList, List: splitLegacyList(raw)}, nil
	case ValueStructuredN:
		parts, err := legacyStructuredParts(raw, structuredNComponents, ValueText)
		if err != nil {
			return Value{}, err
		}
		return Value{Type: ValueStructuredN, Parts: parts}, nil
	case ValueStructuredADR:
		parts, err := legacyStructuredParts(raw, structuredADRComponents, ValueText)
		if err != nil {
			return Value{}, err
		}
		return Value{Type: ValueStructuredADR, Parts: parts}, nil
	case ValueStructuredGender:
		comps := splitEscaped(raw, ';')
		if len(comps) > 2 {
			return Value{}, errors.New("vcard: invalid GENDER value")
		}
		parts := make([]Component, 0, 2)
		parts = append(parts, NewComponent("sex", ValueText, unescapeText(comps[0])))
		if len(comps) == 2 {
			parts = append(parts, NewComponent("identity", ValueText, unescapeText(comps[1])))
		}
		return Value{Type: ValueStructuredGender, Parts: parts}, nil
	case ValueStructuredClientPIDMap:
		comps := splitEscaped(raw, ';')
		if len(comps) != 2 {
			return Value{}, errors.New("vcard: invalid CLIENTPIDMAP value")
		}
		id := unescapeText(comps[0])
		if !reInteger.MatchString(id) {
			return Value{}, errors.New("vcard: invalid CLIENTPIDMAP sourceid")
		}
		parts := []Component{
			NewComponent("sourceid", ValueInteger, id),
			NewComponent("uri", ValueURI, unescapeText(comps[1])),
		}
		return Value{Type: ValueStructuredClientPIDMap, Parts: parts}, nil
	case ValueStructuredOrg:
		comps := splitEscaped(raw, ';')
		list := make([]string, len(comps))
		for i, s := range comps {
			list[i] = unescapeText(s)
		}
		return Value{Type: ValueStructuredOrg, List: list}, nil
	case ValueXML:
		node, err := parseForeignXML(unescapeText(raw))
		if err != nil {
			return Value{}, err
		}
		return Value{Type: ValueXML, XML: node}, nil
	default:
		return Value{Type: typ, Data: raw}, nil
	}
}

func splitLegacyList(s string) []string {
	if s == "" {
		return []string{}
	}
	parts := strings.Split(s, ",")
	return parts
}

func legacyStructuredParts(raw string, names []string, typ string) ([]Component, error) {
	comps := splitEscaped(raw, ';')
	if len(comps) > len(names) {
		return nil, errors.New("vcard: too many structured value components")
	}
	parts := make([]Component, len(names))
	for i, n := range names {
		values := []string{""}
		if i < len(comps) {
			items := splitEscaped(comps[i], ',')
			values = make([]string, len(items))
			for j, it := range items {
				values[j] = unescapeText(it)
			}
		}
		parts[i] = NewComponent(n, typ, values...)
	}
	return parts, nil
}

// parameterTypeHint derives the xCard parameter type from the parameter name
// when converting a legacy Card.
func parameterTypeHint(property, param string, values []string) string {
	switch param {
	case "PREF":
		return ValueInteger
	case "GEO":
		return ValueURI
	case "LANGUAGE":
		return ValueLanguageTag
	case "TZ":
		if len(values) > 0 && looksLikeURI(values[0]) {
			return ValueURI
		}
		return ValueText
	default:
		return ValueText
	}
}

// Card returns a Card representation of the contact. A VERSION 4.0 field is
// always present. Field values are formatted with the legacy text escaping
// conventions so that the existing Encoder reproduces the vCard text.
func (c *Contact) Card() (Card, error) {
	card := make(Card)
	card.Set(FieldVersion, &Field{Value: "4.0"})

	for _, p := range c.Properties {
		f, err := fieldFromProperty(p)
		if err != nil {
			return nil, err
		}
		card.Add(p.Name, f)
	}
	return card, nil
}

func fieldFromProperty(p *Property) (*Field, error) {
	rawValue, rawParams, err := encodePropertyRaw(p)
	if err != nil {
		return nil, err
	}

	params := make(Params)
	for _, rp := range rawParams {
		params[rp.Name] = append(params[rp.Name], rp.Values...)
	}

	// Re-apply the legacy decoder's unescaping so that the legacy Encoder
	// reproduces rawValue exactly.
	return &Field{
		Value:  parseValue(rawValue),
		Params: params,
		Group:  p.Group,
	}, nil
}
