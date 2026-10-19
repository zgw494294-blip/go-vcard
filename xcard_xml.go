package vcard

import (
	"bytes"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"strings"
)

// XCardDecoder reads xCard documents (RFC 6351) and converts each <vcard>
// element to a lossless Contact.
type XCardDecoder struct {
	dec      *xml.Decoder
	started  bool
	finished bool
}

// NewXCardDecoder creates an XCardDecoder reading from r.
func NewXCardDecoder(r io.Reader) *XCardDecoder {
	return &XCardDecoder{dec: xml.NewDecoder(r)}
}

// Decode reads the next contact. It returns io.EOF when no more contacts
// are available.
func (d *XCardDecoder) Decode() (*Contact, error) {
	if d.finished {
		return nil, io.EOF
	}
	if !d.started {
		start, err := readRootStart(d.dec)
		if err != nil {
			return nil, err
		}
		if start.Name.Space != XCardNamespace || start.Name.Local != "vcards" {
			return nil, errors.New("xcard: expected <vcards> element in the vcard-4.0 namespace")
		}
		d.started = true
	}

	for {
		t, err := d.dec.Token()
		if err == io.EOF {
			return nil, io.EOF
		}
		if err != nil {
			return nil, err
		}
		switch t := t.(type) {
		case xml.StartElement:
			if t.Name.Space != XCardNamespace || t.Name.Local != "vcard" {
				return nil, fmt.Errorf("xcard: unexpected element <%s> inside <vcards>", t.Name.Local)
			}
			node, err := buildNode(d.dec, t)
			if err != nil {
				return nil, err
			}
			return contactFromVCardNode(node)
		case xml.EndElement:
			if t.Name.Space == XCardNamespace && t.Name.Local == "vcards" {
				d.finished = true
				return nil, io.EOF
			}
			return nil, errors.New("xcard: unexpected end element")
		case xml.CharData:
			if !isBlankText(string(t)) {
				return nil, errors.New("xcard: unexpected text inside <vcards>")
			}
		case xml.Directive:
			return nil, errors.New("xcard: XML directives are not allowed")
		}
	}
}

func readRootStart(dec *xml.Decoder) (xml.StartElement, error) {
	for {
		t, err := dec.Token()
		if err != nil {
			return xml.StartElement{}, err
		}
		switch t := t.(type) {
		case xml.StartElement:
			return t, nil
		case xml.CharData:
			if !isBlankText(string(t)) {
				return xml.StartElement{}, errors.New("xcard: unexpected text before document element")
			}
		case xml.Directive:
			return xml.StartElement{}, errors.New("xcard: XML directives are not allowed")
		case xml.EndElement:
			return xml.StartElement{}, errors.New("xcard: unexpected end element")
		}
	}
}

func isBlankText(s string) bool {
	return strings.TrimSpace(s) == ""
}

// buildNode consumes a complete element subtree. The start element has
// already been consumed.
func buildNode(dec *xml.Decoder, start xml.StartElement) (*XMLNode, error) {
	node := &XMLNode{
		Name: start.Name,
		Attr: filterNamespaceAttrs(start.Attr),
	}
	for {
		t, err := dec.Token()
		if err != nil {
			return nil, err
		}
		switch t := t.(type) {
		case xml.StartElement:
			child, err := buildNode(dec, t)
			if err != nil {
				return nil, err
			}
			node.AppendElement(child)
		case xml.EndElement:
			return node, nil
		case xml.CharData:
			node.Kids = append(node.Kids, XMLChild{Text: string(t)})
		case xml.Directive:
			return nil, errors.New("xcard: XML directives are not allowed")
		case xml.Comment, xml.ProcInst:
			// Ignored per RFC 6351 section 5.1.
		}
	}
}

// filterNamespaceAttrs removes xmlns/xmlns:* declarations, which
// encoding/xml reports as ordinary attributes in addition to resolving
// element and attribute namespace URIs.
func filterNamespaceAttrs(attrs []xml.Attr) []xml.Attr {
	out := make([]xml.Attr, 0, len(attrs))
	for _, a := range attrs {
		switch a.Name.Space {
		case "xmlns", "http://www.w3.org/2000/xmlns/":
			continue
		}
		if a.Name.Space == "" && a.Name.Local == "xmlns" {
			continue
		}
		out = append(out, a)
	}
	return out
}

// parseForeignXML parses a single foreign-namespaced XML element.
func parseForeignXML(s string) (*XMLNode, error) {
	dec := xml.NewDecoder(strings.NewReader(s))
	start, err := readRootStart(dec)
	if err != nil {
		return nil, err
	}
	node, err := buildNode(dec, start)
	if err != nil {
		return nil, err
	}
	for {
		t, err := dec.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
		switch t := t.(type) {
		case xml.CharData:
			if !isBlankText(string(t)) {
				return nil, errors.New("xcard: trailing text after XML fragment")
			}
		case xml.StartElement:
			return nil, errors.New("xcard: multiple root elements in XML fragment")
		case xml.Directive:
			return nil, errors.New("xcard: XML directives are not allowed")
		}
	}
	if node.Name.Space == XCardNamespace {
		return nil, errors.New("xcard: XML property content must be in a foreign namespace")
	}
	return node, nil
}

// scalar xCard value element names.
var scalarValueElements = map[string]bool{
	"text": true, "uri": true, "boolean": true, "integer": true,
	"float": true, "date": true, "time": true, "date-time": true,
	"date-and-or-time": true, "timestamp": true, "utc-offset": true,
	"language-tag": true, "unknown": true,
}

// contactFromVCardNode converts a <vcard> DOM node to a Contact.
func contactFromVCardNode(vcard *XMLNode) (*Contact, error) {
	if vcard.Name.Space != XCardNamespace || vcard.Name.Local != "vcard" {
		return nil, errors.New("xcard: expected <vcard> element")
	}
	if len(vcard.Attr) != 0 {
		// Attributes on <vcard> have no vCard meaning and are ignored per
		// RFC 6351 section 6.
	}

	c := NewContact()
	for _, kid := range vcard.Kids {
		if kid.Node != nil {
			elem := kid.Node
			if elem.Name.Space != XCardNamespace {
				p := &Property{Name: FieldXML, Value: ForeignValue(elem)}
				c.Add(p)
				continue
			}
			if elem.Name.Local == "group" {
				props, err := parseGroupElement(elem)
				if err != nil {
					return nil, err
				}
				c.Properties = append(c.Properties, props...)
				continue
			}
			if elem.Name.Local == "version" {
				return nil, errors.New("xcard: the version must not be represented as an element")
			}
			if isKnownPropertyElement(elem.Name.Local) || isXName(elem.Name.Local) {
				p, err := parsePropertyElement(elem, "")
				if err != nil {
					return nil, err
				}
				c.Add(p)
				continue
			}
			// Unknown expanded names are ignored per RFC 6351 section 5.1.
		} else if !isBlankText(kid.Text) {
			return nil, errors.New("xcard: unexpected text inside <vcard>")
		}
	}
	return c, nil
}

func isXName(local string) bool {
	return strings.HasPrefix(local, "x-")
}

func isKnownPropertyElement(local string) bool {
	switch local {
	case "source", "kind", "fn", "n", "nickname", "photo", "bday",
		"anniversary", "gender", "adr", "tel", "email", "impp", "lang",
		"tz", "geo", "title", "role", "logo", "org", "member", "related",
		"categories", "note", "prodid", "rev", "sound", "uid",
		"clientpidmap", "url", "key", "fburl", "caladruri", "caluri":
		return true
	}
	return false
}

func parseGroupElement(group *XMLNode) ([]*Property, error) {
	var name string
	for _, a := range group.Attr {
		if a.Name.Space == "" && a.Name.Local == "name" {
			name = a.Value
		}
	}
	if name == "" {
		return nil, errors.New("xcard: <group> element requires a name attribute")
	}

	var props []*Property
	for _, kid := range group.Kids {
		if kid.Node != nil {
			elem := kid.Node
			if elem.Name.Space != XCardNamespace {
				props = append(props, &Property{Group: name, Name: FieldXML, Value: ForeignValue(elem)})
				continue
			}
			if elem.Name.Local == "group" || elem.Name.Local == "version" {
				return nil, fmt.Errorf("xcard: illegal <%s> inside <group>", elem.Name.Local)
			}
			if isKnownPropertyElement(elem.Name.Local) || isXName(elem.Name.Local) {
				p, err := parsePropertyElement(elem, name)
				if err != nil {
					return nil, err
				}
				props = append(props, p)
				continue
			}
		} else if !isBlankText(kid.Text) {
			return nil, errors.New("xcard: unexpected text inside <group>")
		}
	}
	return props, nil
}

func propertyNameFromLocal(local string) string {
	return strings.ToUpper(local)
}

func parsePropertyElement(elem *XMLNode, group string) (*Property, error) {
	p := &Property{Group: group, Name: propertyNameFromLocal(elem.Name.Local)}

	var parametersElem *XMLNode
	for _, kid := range elem.Kids {
		if kid.Node == nil {
			if !isBlankText(kid.Text) {
				return nil, fmt.Errorf("xcard: unexpected text in <%s>", elem.Name.Local)
			}
			continue
		}
		child := kid.Node
		if child.Name.Space == XCardNamespace && child.Name.Local == "parameters" {
			parametersElem = child
			break
		}
	}
	if parametersElem != nil {
		params, err := parseParameters(parametersElem)
		if err != nil {
			return nil, err
		}
		p.Params = params
	}

	switch elem.Name.Local {
	case "n":
		v, err := parseN(elem)
		if err != nil {
			return nil, err
		}
		p.Value = v
	case "adr":
		v, err := parseADR(elem)
		if err != nil {
			return nil, err
		}
		p.Value = v
	case "gender":
		v, err := parseGenderElem(elem)
		if err != nil {
			return nil, err
		}
		p.Value = v
	case "clientpidmap":
		v, err := parseClientPIDMapElem(elem)
		if err != nil {
			return nil, err
		}
		p.Value = v
	case "org":
		v, err := parseOrg(elem)
		if err != nil {
			return nil, err
		}
		p.Value = v
	case "nickname", "categories":
		v, err := parseTextListProperty(elem)
		if err != nil {
			return nil, err
		}
		p.Value = v
	default:
		v, err := parseScalarProperty(elem)
		if err != nil {
			return nil, err
		}
		p.Value = v
	}
	return p, nil
}

func parseParameters(parameters *XMLNode) ([]Parameter, error) {
	var params []Parameter
	for _, kid := range parameters.Kids {
		if kid.Node == nil {
			if !isBlankText(kid.Text) {
				return nil, errors.New("xcard: unexpected text in <parameters>")
			}
			continue
		}
		elem := kid.Node
		if elem.Name.Space != XCardNamespace {
			continue
		}
		pa, err := parseParameterElement(elem)
		if err != nil {
			return nil, err
		}
		params = append(params, pa)
	}
	return params, nil
}

func parseParameterElement(elem *XMLNode) (Parameter, error) {
	name := strings.ToUpper(elem.Name.Local)

	var valueElems []*XMLNode
	for _, kid := range elem.Kids {
		if kid.Node == nil {
			if !isBlankText(kid.Text) {
				return Parameter{}, fmt.Errorf("xcard: unexpected text in parameter <%s>", elem.Name.Local)
			}
			continue
		}
		valueElems = append(valueElems, kid.Node)
	}

	if len(valueElems) == 0 {
		return Parameter{Name: name, Type: ValueText, Values: []string{""}}, nil
	}

	firstType := valueElems[0].Name.Local
	for _, ve := range valueElems {
		if ve.Name.Local != firstType {
			return Parameter{}, fmt.Errorf("xcard: mixed value types in parameter <%s>", elem.Name.Local)
		}
	}
	if !scalarValueElements[firstType] || firstType == "date" || firstType == "time" ||
		firstType == "date-time" || firstType == "timestamp" || firstType == "date-and-or-time" ||
		firstType == "float" || firstType == "boolean" {
		return Parameter{}, fmt.Errorf("xcard: illegal value element <%s> in parameter <%s>", firstType, elem.Name.Local)
	}

	values := make([]string, len(valueElems))
	for i, ve := range valueElems {
		values[i] = elementText(ve)
	}

	typ := ValueText
	if firstType == "unknown" {
		typ = ValueUnknown
	} else if firstType == "integer" {
		typ = ValueInteger
	} else if firstType == "uri" {
		typ = ValueURI
	} else if firstType == "language-tag" {
		typ = ValueLanguageTag
	}

	if err := validateParameterShape(name, typ, len(values), values); err != nil {
		return Parameter{}, err
	}
	return Parameter{Name: name, Type: typ, Values: values}, nil
}

func validateParameterShape(name, typ string, n int, values []string) error {
	switch name {
	case "PREF":
		if typ != ValueInteger || n != 1 || !reInteger.MatchString(values[0]) {
			return errors.New("xcard: PREF parameter must contain one integer")
		}
	case "GEO":
		if typ != ValueURI || n != 1 {
			return errors.New("xcard: GEO parameter must contain one uri")
		}
	case "LANGUAGE":
		if typ != ValueLanguageTag || n != 1 || !reLanguageTag.MatchString(values[0]) {
			return errors.New("xcard: LANGUAGE parameter must contain one language-tag")
		}
	case "TZ":
		if n != 1 || (typ != ValueText && typ != ValueUnknown && typ != ValueURI) {
			return errors.New("xcard: TZ parameter must contain one text or uri")
		}
	case "ALTID", "CALSCALE", "LABEL", "MEDIATYPE":
		if n != 1 || (typ != ValueText && typ != ValueUnknown) {
			return errors.New("xcard: " + name + " parameter must contain one text")
		}
	case "PID", "TYPE", "SORT-AS":
		if typ != ValueText && typ != ValueUnknown {
			return errors.New("xcard: " + name + " parameter must contain text values")
		}
	}
	return nil
}

// elementText returns the concatenated character data of an element,
// dropping descendant elements per RFC 6351 section 6.
func elementText(elem *XMLNode) string {
	var sb strings.Builder
	for _, kid := range elem.Kids {
		if kid.Node == nil {
			sb.WriteString(kid.Text)
		}
	}
	return sb.String()
}

func parseScalarProperty(elem *XMLNode) (Value, error) {
	var candidates []*XMLNode
	for _, kid := range elem.Kids {
		if kid.Node == nil {
			if !isBlankText(kid.Text) {
				return Value{}, fmt.Errorf("xcard: unexpected text in <%s>", elem.Name.Local)
			}
			continue
		}
		child := kid.Node
		if child.Name.Space == XCardNamespace && child.Name.Local == "parameters" {
			continue
		}
		if child.Name.Space == XCardNamespace && scalarValueElements[child.Name.Local] {
			candidates = append(candidates, child)
		}
		// Unrecognized descendants are ignored per RFC 6351 section 6.
	}
	if len(candidates) != 1 {
		return Value{}, fmt.Errorf("xcard: property <%s> must contain exactly one value element", elem.Name.Local)
	}
	ve := candidates[0]
	s := elementText(ve)
	typ := ve.Name.Local
	switch typ {
	case "boolean":
		switch s {
		case "true", "1":
			s = "true"
		case "false", "0":
			s = "false"
		default:
			return Value{}, errors.New("xcard: invalid boolean value: " + s)
		}
	case "integer":
		if !reInteger.MatchString(s) {
			return Value{}, errors.New("xcard: invalid integer value: " + s)
		}
	case "float":
		if !reFloat.MatchString(s) {
			return Value{}, errors.New("xcard: invalid float value: " + s)
		}
	case "utc-offset":
		if !reUTCOffset.MatchString(s) {
			return Value{}, errors.New("xcard: invalid utc-offset value: " + s)
		}
	case "language-tag":
		if !reLanguageTag.MatchString(s) {
			return Value{}, errors.New("xcard: invalid language-tag value: " + s)
		}
	case "date":
		if !reDateBody.MatchString(s) {
			return Value{}, errors.New("xcard: invalid date value: " + s)
		}
	case "time":
		if !reTimeBody.MatchString(s) {
			return Value{}, errors.New("xcard: invalid time value: " + s)
		}
	case "date-time":
		datePart, timePart, ok := splitDateTime(s)
		if !ok || !reDateBody.MatchString(datePart) || !reTimeBody.MatchString(timePart) {
			return Value{}, errors.New("xcard: invalid date-time value: " + s)
		}
	case "timestamp":
		if !reTimestamp.MatchString(s) {
			return Value{}, errors.New("xcard: invalid timestamp value: " + s)
		}
	case "date-and-or-time":
		if !validDateAndOrTimeXML(s) {
			return Value{}, errors.New("xcard: invalid date-and-or-time value: " + s)
		}
	}
	return Value{Type: typ, Data: s}, nil
}

func validDateAndOrTimeXML(s string) bool {
	if reDateBody.MatchString(s) || reTimestamp.MatchString(s) || reTimeBody.MatchString(s) {
		return true
	}
	datePart, timePart, ok := splitDateTime(s)
	return ok && reDateBody.MatchString(datePart) && reTimeBody.MatchString(timePart)
}

func collectComponents(elem *XMLNode, names []string) ([]Component, error) {
	slot := make(map[string][]string)
	for _, kid := range elem.Kids {
		if kid.Node == nil {
			if !isBlankText(kid.Text) {
				return nil, fmt.Errorf("xcard: unexpected text in <%s>", elem.Name.Local)
			}
			continue
		}
		child := kid.Node
		if child.Name.Space != XCardNamespace || child.Name.Local == "parameters" {
			continue
		}
		if !componentName(names, child.Name.Local) {
			continue
		}
		slot[child.Name.Local] = append(slot[child.Name.Local], elementText(child))
	}
	parts := make([]Component, len(names))
	for i, n := range names {
		values := slot[n]
		if values == nil {
			values = []string{""}
		}
		parts[i] = NewComponent(n, ValueText, values...)
	}
	return parts, nil
}

func componentName(names []string, name string) bool {
	for _, n := range names {
		if n == name {
			return true
		}
	}
	return false
}

func parseN(elem *XMLNode) (Value, error) {
	parts, err := collectComponents(elem, structuredNComponents)
	if err != nil {
		return Value{}, err
	}
	return Value{Type: ValueStructuredN, Parts: parts}, nil
}

func parseADR(elem *XMLNode) (Value, error) {
	parts, err := collectComponents(elem, structuredADRComponents)
	if err != nil {
		return Value{}, err
	}
	return Value{Type: ValueStructuredADR, Parts: parts}, nil
}

func parseGenderElem(elem *XMLNode) (Value, error) {
	var sex []string
	var identity []string
	hasSex, hasIdentity := false, false
	for _, kid := range elem.Kids {
		if kid.Node == nil {
			if !isBlankText(kid.Text) {
				return Value{}, errors.New("xcard: unexpected text in <gender>")
			}
			continue
		}
		child := kid.Node
		if child.Name.Space != XCardNamespace {
			continue
		}
		switch child.Name.Local {
		case "sex":
			hasSex = true
			sex = append(sex, elementText(child))
		case "identity":
			hasIdentity = true
			identity = append(identity, elementText(child))
		}
	}
	if !hasSex || len(sex) != 1 {
		return Value{}, errors.New("xcard: <gender> requires exactly one <sex>")
	}
	switch sex[0] {
	case "", "M", "F", "O", "N", "U":
	default:
		return Value{}, errors.New("xcard: invalid GENDER sex: " + sex[0])
	}
	if len(identity) > 1 {
		return Value{}, errors.New("xcard: <gender> allows at most one <identity>")
	}
	parts := []Component{NewComponent("sex", ValueText, sex[0])}
	if hasIdentity {
		parts = append(parts, NewComponent("identity", ValueText, identity[0]))
	}
	return Value{Type: ValueStructuredGender, Parts: parts}, nil
}

func parseClientPIDMapElem(elem *XMLNode) (Value, error) {
	values := map[string][]string{}
	for _, kid := range elem.Kids {
		if kid.Node == nil {
			if !isBlankText(kid.Text) {
				return Value{}, errors.New("xcard: unexpected text in <clientpidmap>")
			}
			continue
		}
		child := kid.Node
		if child.Name.Space != XCardNamespace {
			continue
		}
		switch child.Name.Local {
		case "sourceid":
			values["sourceid"] = append(values["sourceid"], elementText(child))
		case "uri":
			values["uri"] = append(values["uri"], elementText(child))
		}
	}
	id := values["sourceid"]
	uri := values["uri"]
	if len(id) != 1 || !rePositiveInteger.MatchString(id[0]) {
		return Value{}, errors.New("xcard: <clientpidmap> requires one positive integer <sourceid>")
	}
	if len(uri) != 1 || uri[0] == "" {
		return Value{}, errors.New("xcard: <clientpidmap> requires one <uri>")
	}
	parts := []Component{
		NewComponent("sourceid", ValueInteger, id[0]),
		NewComponent("uri", ValueURI, uri[0]),
	}
	return Value{Type: ValueStructuredClientPIDMap, Parts: parts}, nil
}

func parseTextListProperty(elem *XMLNode) (Value, error) {
	var texts []string
	for _, kid := range elem.Kids {
		if kid.Node == nil {
			if !isBlankText(kid.Text) {
				return Value{}, fmt.Errorf("xcard: unexpected text in <%s>", elem.Name.Local)
			}
			continue
		}
		child := kid.Node
		if child.Name.Space == XCardNamespace && child.Name.Local == "parameters" {
			continue
		}
		if child.Name.Space == XCardNamespace && child.Name.Local == "text" {
			texts = append(texts, elementText(child))
			continue
		}
		if child.Name.Space == XCardNamespace && scalarValueElements[child.Name.Local] {
			// A single non-text value is an explicit VALUE override, e.g.
			// NICKNAME;VALUE=uri:...
			v, err := parseScalarProperty(elem)
			if err != nil {
				return Value{}, err
			}
			return v, nil
		}
		// Unrecognized descendants are ignored per RFC 6351 section 6.
	}
	if texts == nil {
		return Value{}, fmt.Errorf("xcard: property <%s> must contain a value element", elem.Name.Local)
	}
	return Value{Type: ValueTextList, List: texts}, nil
}

func parseOrg(elem *XMLNode) (Value, error) {
	var texts []string
	for _, kid := range elem.Kids {
		if kid.Node == nil {
			if !isBlankText(kid.Text) {
				return Value{}, errors.New("xcard: unexpected text in <org>")
			}
			continue
		}
		child := kid.Node
		if child.Name.Space == XCardNamespace && child.Name.Local == "parameters" {
			continue
		}
		if child.Name.Space == XCardNamespace && child.Name.Local == "text" {
			texts = append(texts, elementText(child))
		}
	}
	return Value{Type: ValueStructuredOrg, List: texts}, nil
}

// ---------------------------------------------------------------------------
// XML serialization
// ---------------------------------------------------------------------------

// XCardEncoder writes contacts as an xCard document (RFC 6351). The
// enclosing <vcards> element is opened on the first Encode call and closed
// with Close.
type XCardEncoder struct {
	w       io.Writer
	started bool
	p       *xmlPrinter
}

// NewXCardEncoder creates an XCardEncoder writing to w.
func NewXCardEncoder(w io.Writer) *XCardEncoder {
	return &XCardEncoder{w: w, p: newXMLPrinter("  ")}
}

// Encode writes one contact as a <vcard> element.
func (e *XCardEncoder) Encode(c *Contact) error {
	if !e.started {
		e.p.writeString(xmlHeader)
		e.p.startElement(XCardNamespace, "vcards", nil)
		e.started = true
	}
	if err := e.writeVCard(c); err != nil {
		return err
	}
	_, err := io.WriteString(e.w, e.p.takeOutput())
	return err
}

// Close closes the <vcards> element.
func (e *XCardEncoder) Close() error {
	if !e.started {
		e.p.writeString(xmlHeader)
		e.p.startElement(XCardNamespace, "vcards", nil)
		e.started = true
	}
	e.p.endElement()
	_, err := io.WriteString(e.w, e.p.takeOutput())
	return err
}

const xmlHeader = `<?xml version="1.0" encoding="UTF-8"?>` + "\n"

func (e *XCardEncoder) writeVCard(c *Contact) error {
	e.p.startElement(XCardNamespace, "vcard", nil)

	i := 0
	for i < len(c.Properties) {
		p := c.Properties[i]
		group := p.Group
		if group == "" {
			if err := e.writeProperty(p); err != nil {
				return err
			}
			i++
			continue
		}
		e.p.startElement(XCardNamespace, "group", []xml.Attr{
			{Name: xml.Name{Local: "name"}, Value: group},
		})
		for i < len(c.Properties) && c.Properties[i].Group == group {
			if err := e.writeProperty(c.Properties[i]); err != nil {
				return err
			}
			i++
		}
		e.p.endElement()
	}

	e.p.endElement()
	return nil
}

func (e *XCardEncoder) writeProperty(p *Property) error {
	if p.Value.Type == ValueXML {
		if len(p.Params) != 0 {
			return errors.New("xcard: embedded XML properties cannot carry parameters")
		}
		if p.Value.XML == nil {
			return errors.New("xcard: missing XML node")
		}
		e.p.writeNode(p.Value.XML)
		return nil
	}

	local := strings.ToLower(p.Name)
	inline := len(p.Params) == 0 && isInlineValue(p.Value)
	if inline {
		e.p.inlineOpen(XCardNamespace, local, nil)
		if err := e.writePropertyValue(p.Name, p.Value); err != nil {
			return err
		}
		e.p.inlineClose()
		return nil
	}

	e.p.startElement(XCardNamespace, local, nil)

	if len(p.Params) > 0 {
		e.p.startElement(XCardNamespace, "parameters", nil)
		for _, pa := range p.Params {
			if err := e.writeParameter(pa); err != nil {
				return err
			}
		}
		e.p.endElement()
	}

	if err := e.writePropertyValue(p.Name, p.Value); err != nil {
		return err
	}

	e.p.endElement()
	return nil
}

// isInlineValue reports whether a value consists solely of leaf elements and
// can be rendered on one line. N and ADR keep their RFC-style tree layout.
func isInlineValue(v Value) bool {
	switch v.Type {
	case ValueStructuredN, ValueStructuredADR:
		return false
	default:
		return true
	}
}

func (e *XCardEncoder) writeParameter(pa Parameter) error {
	local := strings.ToLower(pa.Name)

	values := pa.Values
	if len(values) == 0 {
		values = []string{""}
	}
	childType := pa.Type
	if childType == "" {
		childType = ValueText
	}
	switch childType {
	case ValueInteger, ValueURI, ValueLanguageTag:
		if len(values) != 1 {
			return fmt.Errorf("xcard: parameter %s takes a single value", pa.Name)
		}
		if childType == ValueInteger && !reInteger.MatchString(values[0]) {
			return fmt.Errorf("xcard: invalid integer for parameter %s: %s", pa.Name, values[0])
		}
		if childType == ValueLanguageTag && !reLanguageTag.MatchString(values[0]) {
			return fmt.Errorf("xcard: invalid language-tag for parameter %s: %s", pa.Name, values[0])
		}
	}

	// Single-value parameters stay on one line, matching the RFC examples;
	// multi-valued parameters (e.g. TYPE) keep each value on its own line.
	inline := len(values) == 1
	if inline {
		e.p.inlineOpen(XCardNamespace, local, nil)
	} else {
		e.p.startElement(XCardNamespace, local, nil)
	}

	switch childType {
	case ValueText, ValueUnknown:
		childLocal := "text"
		if childType == ValueUnknown {
			childLocal = "unknown"
		}
		for _, v := range values {
			e.p.writeLeaf(XCardNamespace, childLocal, v)
		}
	case ValueInteger, ValueURI, ValueLanguageTag:
		e.p.writeLeaf(XCardNamespace, childType, values[0])
	default:
		return fmt.Errorf("xcard: unsupported parameter type: %s", childType)
	}

	if inline {
		e.p.inlineClose()
	} else {
		e.p.endElement()
	}
	return nil
}

func (e *XCardEncoder) writePropertyValue(name string, v Value) error {
	switch v.Type {
	case ValueStructuredN, ValueStructuredADR:
		return e.writeStructured(v)
	case ValueStructuredGender:
		return e.writeGender(v)
	case ValueStructuredClientPIDMap:
		return e.writeClientPIDMap(v)
	case ValueStructuredOrg, ValueTextList:
		for _, t := range v.List {
			e.p.writeLeaf(XCardNamespace, "text", t)
		}
		return nil
	default:
		return e.writeScalar(v)
	}
}

func (e *XCardEncoder) writeStructured(v Value) error {
	var names []string
	if v.Type == ValueStructuredN {
		names = structuredNComponents
	} else {
		names = structuredADRComponents
	}
	byName := map[string]Component{}
	for _, part := range v.Parts {
		byName[part.Name] = part
	}
	for _, n := range names {
		part, ok := byName[n]
		values := []string{""}
		if ok && len(part.Values) > 0 {
			values = part.Values
		}
		for _, s := range values {
			e.p.writeLeaf(XCardNamespace, n, s)
		}
	}
	return nil
}

func (e *XCardEncoder) writeGender(v Value) error {
	sex := ""
	var identity []string
	for _, part := range v.Parts {
		switch part.Name {
		case "sex":
			if len(part.Values) > 0 {
				sex = part.Values[0]
			}
		case "identity":
			identity = part.Values
		}
	}
	e.p.writeLeaf(XCardNamespace, "sex", sex)
	if identity != nil {
		s := ""
		if len(identity) > 0 {
			s = identity[0]
		}
		e.p.writeLeaf(XCardNamespace, "identity", s)
	}
	return nil
}

func (e *XCardEncoder) writeClientPIDMap(v Value) error {
	var id, uri string
	for _, part := range v.Parts {
		s := ""
		if len(part.Values) > 0 {
			s = part.Values[0]
		}
		switch part.Name {
		case "sourceid":
			id = s
		case "uri":
			uri = s
		}
	}
	e.p.writeLeaf(XCardNamespace, "sourceid", id)
	e.p.writeLeaf(XCardNamespace, "uri", uri)
	return nil
}

func (e *XCardEncoder) writeScalar(v Value) error {
	local := v.Type
	data := v.Data
	switch v.Type {
	case ValueBoolean:
		if strings.EqualFold(data, "true") || data == "1" {
			data = "true"
		} else if strings.EqualFold(data, "false") || data == "0" {
			data = "false"
		} else {
			return errors.New("xcard: invalid boolean value: " + data)
		}
	case ValueTime:
		data = strings.TrimPrefix(data, "T")
		if err := validateScalarText(ValueTime, "T"+data); err != nil {
			return err
		}
	case ValueDateAndOrTime:
		if strings.HasPrefix(data, "T") {
			data = data[1:]
		}
		if err := validateScalarText(ValueDateAndOrTime, data); err != nil {
			return err
		}
	case ValueInteger, ValueFloat, ValueDate, ValueDateTime, ValueTimestamp,
		ValueUTCOffset, ValueLanguageTag:
		if err := validateScalarText(v.Type, data); err != nil {
			return err
		}
	case ValueText, ValueURI, ValueUnknown:
		// No lexical restrictions.
	default:
		return errors.New("xcard: unsupported value type: " + v.Type)
	}
	e.p.writeLeaf(XCardNamespace, local, data)
	return nil
}

// ---------------------------------------------------------------------------
// Low-level namespace-aware XML printer
// ---------------------------------------------------------------------------

type nsFrame struct {
	defaultURI string
	prefixes   map[string]string // URI -> prefix at this frame
}

type xmlPrinter struct {
	buf         bytes.Buffer
	indent      string
	level       int
	frames      []nsFrame
	counter     int
	pending     string // indentation to emit before the next content
	openElems   []openElem
	inlineDepth int
}

type openElem struct {
	uri     string
	local   string
	hasKids bool
}

func newXMLPrinter(indent string) *xmlPrinter {
	p := &xmlPrinter{indent: indent}
	p.frames = []nsFrame{{defaultURI: "", prefixes: map[string]string{}}}
	return p
}

func (p *xmlPrinter) writeString(s string) {
	p.buf.WriteString(s)
}

func (p *xmlPrinter) takeOutput() string {
	s := p.buf.String()
	p.buf.Reset()
	return s
}

func (p *xmlPrinter) currentFrame() *nsFrame {
	return &p.frames[len(p.frames)-1]
}

func (p *xmlPrinter) lookupPrefix(uri string) (string, bool) {
	for i := len(p.frames) - 1; i >= 0; i-- {
		if p.frames[i].defaultURI == uri {
			return "", true
		}
		if pref, ok := p.frames[i].prefixes[uri]; ok {
			return pref, true
		}
	}
	return "", false
}

func (p *xmlPrinter) newPrefix() string {
	p.counter++
	return fmt.Sprintf("x%d", p.counter)
}

// resolveDecls computes namespace declarations needed for an element and
// its attributes. It returns: prefix for the element name, the default URI
// declared at this element (if any), prefixed declarations, and the prefix
// of each attribute.
func (p *xmlPrinter) resolveDecls(uri string, attrs []xml.Attr) (elemPrefix string, declDefault string, declPrefix map[string]string, attrPrefix []string) {
	declPrefix = map[string]string{}

	if pref, ok := p.lookupPrefix(uri); ok {
		elemPrefix = pref
	} else {
		// The element's own namespace becomes the default; attribute
		// namespaces are bound to generated prefixes below. Both can be
		// declared on the same element.
		declDefault = uri
	}

	attrPrefix = make([]string, len(attrs))
	for i, a := range attrs {
		if a.Name.Space == "" {
			continue
		}
		if pref, ok := p.lookupAttrPrefix(a.Name.Space); ok {
			attrPrefix[i] = pref
			continue
		}
		if pref, ok := declPrefix[a.Name.Space]; ok {
			attrPrefix[i] = pref
			continue
		}
		pref := p.newPrefix()
		declPrefix[a.Name.Space] = pref
		attrPrefix[i] = pref
	}
	return
}

// lookupAttrPrefix finds a prefix for an attribute namespace. Default
// namespaces never apply to attributes, so they are not considered.
func (p *xmlPrinter) lookupAttrPrefix(uri string) (string, bool) {
	for i := len(p.frames) - 1; i >= 0; i-- {
		if pref, ok := p.frames[i].prefixes[uri]; ok {
			return pref, true
		}
	}
	return "", false
}

func (p *xmlPrinter) pushFrame(declDefault string, declPrefix map[string]string) {
	prev := p.frames[len(p.frames)-1]
	f := nsFrame{defaultURI: prev.defaultURI, prefixes: map[string]string{}}
	if declDefault != "" {
		f.defaultURI = declDefault
	}
	for uri, pref := range declPrefix {
		f.prefixes[uri] = pref
	}
	p.frames = append(p.frames, f)
}

func (p *xmlPrinter) popFrame() {
	p.frames = p.frames[:len(p.frames)-1]
}

func writeEscapedText(w *bytes.Buffer, s string) {
	var esc bytes.Buffer
	if err := xml.EscapeText(&esc, []byte(s)); err != nil {
		// EscapeText only errors on unknown charsets, which cannot occur
		// with bytes.Buffer.
		panic(err)
	}
	w.Write(esc.Bytes())
}

func (p *xmlPrinter) writeIndent() {
	if p.indent != "" {
		if p.buf.Len() == 0 || p.buf.Bytes()[p.buf.Len()-1] != '\n' {
			p.buf.WriteByte('\n')
		}
		for i := 0; i < p.level; i++ {
			p.buf.WriteString(p.indent)
		}
	}
}

// startElement opens an element. Layout: elements with element children are
// pretty-printed; leaf elements stay inline.
func (p *xmlPrinter) startElement(uri, local string, attrs []xml.Attr) {
	if len(p.openElems) > 0 {
		p.openElems[len(p.openElems)-1].hasKids = true
	}
	p.writeIndent()
	p.writeStartTag(uri, local, attrs, false)
	p.level++
	p.openElems = append(p.openElems, openElem{uri: uri, local: local})
}

func (p *xmlPrinter) endElement() {
	elem := p.openElems[len(p.openElems)-1]
	p.openElems = p.openElems[:len(p.openElems)-1]
	p.level--
	if elem.hasKids {
		p.writeIndent()
	}
	p.buf.WriteString("</")
	if pref, ok := p.lookupPrefix(elem.uri); ok && pref != "" {
		p.buf.WriteString(pref)
		p.buf.WriteByte(':')
	}
	writeEscapedName(&p.buf, elem.local)
	p.buf.WriteByte('>')
	p.popFrame()
}

func writeEscapedName(w *bytes.Buffer, s string) {
	w.WriteString(s)
}

func (p *xmlPrinter) writeStartTag(uri, local string, attrs []xml.Attr, selfClose bool) {
	elemPrefix, declDefault, declPrefix, attrPrefix := p.resolveDecls(uri, attrs)
	p.pushFrame(declDefault, declPrefix)

	p.buf.WriteByte('<')
	if elemPrefix != "" {
		p.buf.WriteString(elemPrefix)
		p.buf.WriteByte(':')
	}
	p.buf.WriteString(local)

	if declDefault != "" {
		p.buf.WriteString(` xmlns="`)
		writeEscapedText(&p.buf, declDefault)
		p.buf.WriteByte('"')
	}
	uris := make([]string, 0, len(declPrefix))
	for u := range declPrefix {
		uris = append(uris, u)
	}
	// Stable ordering of generated prefixes.
	sortStrings(uris)
	for _, u := range uris {
		p.buf.WriteString(` xmlns:`)
		p.buf.WriteString(declPrefix[u])
		p.buf.WriteString(`="`)
		writeEscapedText(&p.buf, u)
		p.buf.WriteByte('"')
	}

	for i, a := range attrs {
		if a.Name.Local == "xmlns" && a.Name.Space == "" {
			continue
		}
		p.buf.WriteByte(' ')
		if attrPrefix[i] != "" {
			p.buf.WriteString(attrPrefix[i])
			p.buf.WriteByte(':')
		}
		p.buf.WriteString(a.Name.Local)
		p.buf.WriteString(`="`)
		writeEscapedText(&p.buf, a.Value)
		p.buf.WriteByte('"')
	}
	if selfClose {
		p.buf.WriteString("/>")
	} else {
		p.buf.WriteByte('>')
	}
}

func sortStrings(s []string) {
	for i := 1; i < len(s); i++ {
		for j := i; j > 0 && s[j-1] > s[j]; j-- {
			s[j-1], s[j] = s[j], s[j-1]
		}
	}
}

// writeLeaf writes a complete element with only text content. Empty text
// produces a self-closing element.
func (p *xmlPrinter) writeLeaf(uri, local, text string) {
	if len(p.openElems) > 0 {
		p.openElems[len(p.openElems)-1].hasKids = true
	}
	if p.inlineDepth == 0 {
		p.writeIndent()
	}
	if text == "" {
		p.writeStartTag(uri, local, nil, true)
		p.popFrame()
		return
	}
	p.writeStartTag(uri, local, nil, false)
	writeEscapedText(&p.buf, text)
	p.buf.WriteString("</")
	if pref, ok := p.lookupPrefix(uri); ok && pref != "" {
		p.buf.WriteString(pref)
		p.buf.WriteByte(':')
	}
	p.buf.WriteString(local)
	p.buf.WriteByte('>')
	p.popFrame()
}

// inlineOpen opens an element whose content stays on one line.
func (p *xmlPrinter) inlineOpen(uri, local string, attrs []xml.Attr) {
	if len(p.openElems) > 0 {
		p.openElems[len(p.openElems)-1].hasKids = true
	}
	p.writeIndent()
	p.writeStartTag(uri, local, attrs, false)
	p.openElems = append(p.openElems, openElem{uri: uri, local: local})
	p.inlineDepth++
}

// inlineClose closes an element opened with inlineOpen.
func (p *xmlPrinter) inlineClose() {
	elem := p.openElems[len(p.openElems)-1]
	p.openElems = p.openElems[:len(p.openElems)-1]
	p.inlineDepth--
	p.buf.WriteString("</")
	if pref, ok := p.lookupPrefix(elem.uri); ok && pref != "" {
		p.buf.WriteString(pref)
		p.buf.WriteByte(':')
	}
	p.buf.WriteString(elem.local)
	p.buf.WriteByte('>')
	p.popFrame()
}

// writeNode writes a foreign-namespaced DOM node.
func (p *xmlPrinter) writeNode(node *XMLNode) {
	if len(p.openElems) > 0 {
		p.openElems[len(p.openElems)-1].hasKids = true
	}

	hasElements := false
	for _, k := range node.Kids {
		if k.Node != nil {
			hasElements = true
			break
		}
	}

	p.writeIndent()
	p.writeStartTag(node.Name.Space, node.Name.Local, node.Attr, false)
	if !hasElements {
		for _, k := range node.Kids {
			if k.Node == nil {
				writeEscapedText(&p.buf, k.Text)
			}
		}
		p.buf.WriteString("</")
		if pref, ok := p.lookupPrefix(node.Name.Space); ok && pref != "" {
			p.buf.WriteString(pref)
			p.buf.WriteByte(':')
		}
		p.buf.WriteString(node.Name.Local)
		p.buf.WriteByte('>')
		p.popFrame()
		return
	}

	p.level++
	p.openElems = append(p.openElems, openElem{uri: node.Name.Space, local: node.Name.Local, hasKids: true})
	for _, k := range node.Kids {
		if k.Node != nil {
			p.writeNode(k.Node)
		} else if !isBlankText(k.Text) {
			if p.indent == "" {
				writeEscapedText(&p.buf, k.Text)
			} else {
				p.writeIndent()
				writeEscapedText(&p.buf, k.Text)
			}
		}
	}
	p.endElement()
}

// serializeXMLNodeCompact renders a foreign node without indentation.
func serializeXMLNodeCompact(node *XMLNode) (string, error) {
	p := newXMLPrinter("")
	p.writeNode(node)
	return p.takeOutput(), nil
}

// MarshalXCard renders contacts as a complete xCard document.
func MarshalXCard(contacts []*Contact) ([]byte, error) {
	var buf bytes.Buffer
	enc := NewXCardEncoder(&buf)
	for _, c := range contacts {
		if err := enc.Encode(c); err != nil {
			return nil, err
		}
	}
	if err := enc.Close(); err != nil {
		return nil, err
	}
	return []byte(buf.String()), nil
}

// UnmarshalXCard parses a complete xCard document and returns all contacts.
func UnmarshalXCard(data []byte) ([]*Contact, error) {
	dec := NewXCardDecoder(bytes.NewReader(data))
	var contacts []*Contact
	for {
		c, err := dec.Decode()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
		contacts = append(contacts, c)
	}
	if len(contacts) == 0 {
		return nil, errors.New("xcard: document contains no vcard")
	}
	return contacts, nil
}
