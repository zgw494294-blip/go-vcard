package vcard

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"regexp"
	"strings"
)

// VCard4Decoder reads vCard 4 contacts in plain text (RFC 6350) and
// converts them to the lossless Contact representation. It is an
// independent, stricter companion to Decoder: folding, quoting and value
// escaping are handled with full awareness of the vCard grammar, so
// structural separators and escaped literal separators are distinguished.
type VCard4Decoder struct {
	r *bufio.Reader
}

// NewVCard4Decoder creates a VCard4Decoder reading from r.
func NewVCard4Decoder(r io.Reader) *VCard4Decoder {
	return &VCard4Decoder{r: bufio.NewReader(r)}
}

// Decode reads the next contact. It returns io.EOF when no more contacts
// are available.
func (d *VCard4Decoder) Decode() (*Contact, error) {
	for {
		line, err := d.readLogicalLine()
		if err != nil {
			return nil, err
		}
		if strings.TrimSpace(line) == "" {
			continue
		}

		group, name, _, rawValue, perr := parseContentLine(line)
		if perr != nil {
			return nil, perr
		}
		if group != "" || !strings.EqualFold(name, "BEGIN") || !strings.EqualFold(rawValue, "VCARD") {
			return nil, errors.New("vcard: expected BEGIN:VCARD")
		}
		break
	}

	contact := NewContact()
	hasVersion := false
	for {
		line, err := d.readLogicalLine()
		if err != nil {
			if err == io.EOF {
				return nil, errors.New("vcard: unexpected EOF, no END found")
			}
			return nil, err
		}
		if line == "" {
			return nil, errors.New("vcard: empty line inside vcard")
		}

		group, name, params, rawValue, perr := parseContentLine(line)
		if perr != nil {
			return nil, perr
		}
		upper := strings.ToUpper(name)

		if upper == "END" {
			if !strings.EqualFold(rawValue, "VCARD") {
				return nil, errors.New("vcard: invalid END value")
			}
			if !hasVersion {
				return nil, errors.New("vcard: missing VERSION property")
			}
			return contact, nil
		}
		if upper == "BEGIN" {
			return nil, errors.New("vcard: unexpected BEGIN inside vcard")
		}
		if upper == FieldVersion {
			if group != "" || len(params) != 0 || !strings.HasPrefix(rawValue, "4.") {
				return nil, errors.New("vcard: unsupported vCard version")
			}
			hasVersion = true
			continue
		}

		p, err := buildProperty(group, upper, params, rawValue)
		if err != nil {
			return nil, err
		}
		contact.Add(p)
	}
}

// readLogicalLine reads one physical line and unfolds continuation lines
// (those beginning with a space or horizontal tab).
func (d *VCard4Decoder) readLogicalLine() (string, error) {
	line, err := d.r.ReadString('\n')
	if len(line) == 0 && err != nil {
		return "", err
	}
	line = strings.TrimRight(line, "\r\n")
	if err != nil && err != io.EOF {
		return line, err
	}

	for {
		next, perr := d.r.Peek(1)
		if perr != nil {
			if perr == io.EOF {
				break
			}
			return line, perr
		}
		if next[0] != ' ' && next[0] != '\t' {
			break
		}
		if _, err := d.r.Discard(1); err != nil {
			return line, err
		}
		folded, ferr := d.r.ReadString('\n')
		line += strings.TrimRight(folded, "\r\n")
		if ferr != nil {
			if ferr == io.EOF {
				break
			}
			return line, ferr
		}
	}
	return line, nil
}

var reNameToken = regexp.MustCompile(`^[A-Za-z0-9\-]+$`)

// parseContentLine splits one unfolded content line into its group, name,
// parameters and the raw, still-escaped value text.
func parseContentLine(line string) (group, name string, params []rawInputParam, value string, err error) {
	rest := line
	if i := strings.IndexByte(rest, '.'); i >= 0 {
		candidate := rest[:i]
		if reNameToken.MatchString(candidate) {
			group = candidate
			rest = rest[i+1:]
		}
	}

	i := strings.IndexAny(rest, ";:")
	if i < 0 {
		err = errors.New("vcard: malformed content line")
		return
	}
	name = rest[:i]
	if !reNameToken.MatchString(name) {
		err = errors.New("vcard: invalid property name")
		return
	}
	separator := rest[i]
	rest = rest[i+1:]

	if separator == ';' {
		params, value, err = parseInputParams(rest)
		if err != nil {
			return
		}
	} else {
		value = rest
	}
	return
}

type rawInputParam struct {
	name   string
	values []string
}

func parseInputParams(s string) (params []rawInputParam, value string, err error) {
	for {
		eq := strings.IndexByte(s, '=')
		sep := strings.IndexAny(s, ";:")
		if eq < 0 || (sep >= 0 && sep < eq) {
			return nil, "", errors.New("vcard: malformed parameter list")
		}
		name := s[:eq]
		if !reNameToken.MatchString(name) {
			return nil, "", errors.New("vcard: invalid parameter name")
		}
		s = s[eq+1:]

		var values []string
		values, s, err = parseInputParamValues(s)
		if err != nil {
			return
		}
		params = append(params, rawInputParam{name: strings.ToUpper(name), values: values})

		if s == "" {
			return nil, "", errors.New("vcard: malformed parameter list")
		}
		if s[0] == ':' {
			return params, s[1:], nil
		}
		s = s[1:] // skip ';'
	}
}

func parseInputParamValues(s string) (values []string, rest string, err error) {
	rest = s
	for {
		if rest == "" {
			return nil, "", errors.New("vcard: malformed parameter value")
		}
		if rest[0] == '"' {
			var v string
			v, rest, err = parseQuotedParam(rest[1:])
			if err != nil {
				return
			}
			values = append(values, v)
		} else {
			i := strings.IndexAny(rest, ",;:")
			if i < 0 {
				return nil, "", errors.New("vcard: malformed parameter value")
			}
			values = append(values, unescapeParamToken(rest[:i]))
			rest = rest[i:]
		}

		if rest == "" {
			return nil, "", errors.New("vcard: malformed parameter value")
		}
		switch rest[0] {
		case ',':
			rest = rest[1:]
		case ';', ':':
			return values, rest, nil
		default:
			return nil, "", errors.New("vcard: malformed parameter value")
		}
	}
}

func parseQuotedParam(s string) (value, rest string, err error) {
	var sb strings.Builder
	for i := 0; i < len(s); {
		c := s[i]
		if c == '"' {
			return sb.String(), s[i+1:], nil
		}
		if c == '\\' && i+1 < len(s) {
			switch s[i+1] {
			case 'n', 'N':
				sb.WriteByte('\n')
			case 'r', 'R':
				sb.WriteByte('\r')
			case 't', 'T':
				sb.WriteByte('\t')
			default:
				sb.WriteByte(s[i+1])
			}
			i += 2
			continue
		}
		sb.WriteByte(c)
		i++
	}
	return "", "", errors.New("vcard: unterminated quoted parameter value")
}

func unescapeParamToken(s string) string {
	return unescapeText(s)
}

// splitEscaped splits s on unescaped occurrences of sep. Backslash escapes
// the following byte.
func splitEscaped(s string, sep byte) []string {
	var out []string
	var sb strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] == '\\' && i+1 < len(s) {
			sb.WriteByte(s[i])
			sb.WriteByte(s[i+1])
			i++
			continue
		}
		if s[i] == sep {
			out = append(out, sb.String())
			sb.Reset()
			continue
		}
		sb.WriteByte(s[i])
	}
	out = append(out, sb.String())
	return out
}

// unescapeText undoes vCard property value escaping (RFC 6350 section 3.3).
func unescapeText(s string) string {
	var sb strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] == '\\' && i+1 < len(s) {
			switch s[i+1] {
			case 'n', 'N':
				sb.WriteByte('\n')
			case 'r', 'R':
				sb.WriteByte('\r')
			default:
				sb.WriteByte(s[i+1])
			}
			i++
			continue
		}
		sb.WriteByte(s[i])
	}
	return sb.String()
}

// escapeText escapes a vCard text value. Structural characters (backslash,
// newline, comma, semicolon) are always escaped, which is legal in every
// context.
func escapeText(s string) string {
	var sb strings.Builder
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case '\\':
			sb.WriteString(`\\`)
		case '\n':
			sb.WriteString(`\n`)
		case '\r':
			sb.WriteString(`\r`)
		case ',':
			sb.WriteString(`\,`)
		case ';':
			sb.WriteString(`\;`)
		default:
			sb.WriteByte(s[i])
		}
	}
	return sb.String()
}

// escapeXMLPropertyValue escapes an embedded XML fragment for use as a vCard
// property value. Semicolons are not escaped: they are not structural for
// the XML property and frequently occur inside URIs.
func escapeXMLPropertyValue(s string) string {
	var sb strings.Builder
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case '\\':
			sb.WriteString(`\\`)
		case '\n':
			sb.WriteString(`\n`)
		case '\r':
			sb.WriteString(`\r`)
		case ',':
			sb.WriteString(`\,`)
		default:
			sb.WriteByte(s[i])
		}
	}
	return sb.String()
}

// Structured component element names (RFC 6351 Appendix A).
var (
	structuredNComponents   = []string{"surname", "given", "additional", "prefix", "suffix"}
	structuredADRComponents = []string{"pobox", "ext", "street", "locality", "region", "code", "country"}
)

// defaultValueType returns the default vCard 4 value type token of a known
// property. The second result is false for unrecognized properties.
func defaultValueType(name string) (string, bool) {
	switch strings.ToUpper(name) {
	case FieldSource, FieldPhoto, FieldTelephone, FieldIMPP, FieldGeolocation,
		FieldLogo, FieldMember, FieldRelated, FieldSound, FieldUID, FieldURL,
		FieldKey, FieldFreeOrBusyURL, FieldCalendarAddressURI, FieldCalendarURI:
		return ValueURI, true
	case FieldKind, FieldFormattedName, FieldEmail, FieldTitle, FieldRole,
		FieldNote, FieldProductID, FieldVersion:
		return ValueText, true
	case FieldName:
		return ValueStructuredN, true
	case FieldAddress:
		return ValueStructuredADR, true
	case FieldNickname, FieldCategories:
		return ValueTextList, true
	case FieldBirthday, FieldAnniversary:
		return ValueDateAndOrTime, true
	case FieldGender:
		return ValueStructuredGender, true
	case FieldLanguage:
		return ValueLanguageTag, true
	case FieldTimezone:
		return ValueUTCOffset, true
	case FieldOrganization:
		return ValueStructuredOrg, true
	case FieldRevision:
		return ValueTimestamp, true
	case FieldClientPIDMap:
		return ValueStructuredClientPIDMap, true
	default:
		return "", false
	}
}

func isScalarValueType(t string) bool {
	switch t {
	case ValueText, ValueURI, ValueBoolean, ValueInteger, ValueFloat,
		ValueDate, ValueTime, ValueDateTime, ValueDateAndOrTime,
		ValueTimestamp, ValueUTCOffset, ValueLanguageTag:
		return true
	}
	return false
}

// buildProperty constructs a Property from a parsed content line.
func buildProperty(group, name string, rawParams []rawInputParam, rawValue string) (*Property, error) {
	p := &Property{Group: group, Name: name}

	explicitType := ""
	for _, rp := range rawParams {
		if rp.name == ParamValue {
			if len(rp.values) != 1 {
				return nil, errors.New("vcard: VALUE parameter must have a single value")
			}
			explicitType = strings.ToLower(rp.values[0])
			continue
		}
		p.Params = append(p.Params, inputParameter(name, rp))
	}

	if name == FieldXML {
		if explicitType != "" {
			return nil, errors.New("vcard: XML property must not have a VALUE parameter")
		}
		if len(p.Params) != 0 {
			return nil, errors.New("vcard: XML property parameters cannot be represented in xCard")
		}
		node, err := parseForeignXML(unescapeText(rawValue))
		if err != nil {
			return nil, fmt.Errorf("vcard: invalid XML property: %w", err)
		}
		p.Value = ForeignValue(node)
		return p, nil
	}

	typ, known := defaultValueType(name)
	if explicitType != "" {
		if !isScalarValueType(explicitType) {
			return nil, errors.New("vcard: unknown VALUE type: " + explicitType)
		}
		if isStructuredType(typ) {
			return nil, errors.New("vcard: " + name + " does not allow a VALUE parameter")
		}
		typ = explicitType
	} else if !known {
		typ = ValueUnknown
	}

	v, err := parsePropertyValue(typ, rawValue)
	if err != nil {
		return nil, err
	}
	p.Value = v
	return p, nil
}

func isStructuredType(t string) bool {
	switch t {
	case ValueStructuredN, ValueStructuredADR, ValueStructuredGender,
		ValueStructuredClientPIDMap, ValueStructuredOrg:
		return true
	}
	return false
}

func inputParameter(property string, rp rawInputParam) Parameter {
	typ := ValueUnknown
	switch rp.name {
	case "PREF":
		typ = ValueInteger
	case "GEO":
		typ = ValueURI
	case "LANGUAGE":
		typ = ValueLanguageTag
	case "TZ":
		if len(rp.values) > 0 && looksLikeURI(rp.values[0]) {
			typ = ValueURI
		} else {
			typ = ValueText
		}
	case "ALTID", "CALSCALE", "LABEL", "MEDIATYPE", "PID", "SORT-AS", "TYPE":
		typ = ValueText
	default:
		typ = ValueUnknown
	}
	return Parameter{Name: rp.name, Type: typ, Values: rp.values}
}

func looksLikeURI(s string) bool {
	i := strings.IndexByte(s, ':')
	if i <= 0 {
		return false
	}
	scheme := s[:i]
	for i := 0; i < len(scheme); i++ {
		c := scheme[i]
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' ||
			c >= '0' && c <= '9' || c == '+' || c == '-' || c == '.') {
			return false
		}
	}
	return true
}

func parsePropertyValue(typ, raw string) (Value, error) {
	switch typ {
	case ValueText:
		return Value{Type: ValueText, Data: unescapeText(raw)}, nil
	case ValueTextList:
		items := splitEscaped(raw, ',')
		values := make([]string, len(items))
		for i, it := range items {
			values[i] = unescapeText(it)
		}
		return Value{Type: ValueTextList, List: values}, nil
	case ValueUnknown:
		// The content is kept unprocessed; only vCard escaping is undone.
		return Value{Type: ValueUnknown, Data: unescapeText(raw)}, nil
	case ValueStructuredN:
		parts, err := parseStructuredComponents(raw, structuredNComponents)
		if err != nil {
			return Value{}, err
		}
		return Value{Type: ValueStructuredN, Parts: parts}, nil
	case ValueStructuredADR:
		parts, err := parseStructuredComponents(raw, structuredADRComponents)
		if err != nil {
			return Value{}, err
		}
		return Value{Type: ValueStructuredADR, Parts: parts}, nil
	case ValueStructuredGender:
		return parseGender(raw)
	case ValueStructuredClientPIDMap:
		return parseClientPIDMap(raw)
	case ValueStructuredOrg:
		items := splitEscaped(raw, ';')
		values := make([]string, len(items))
		for i, it := range items {
			values[i] = unescapeText(it)
		}
		return Value{Type: ValueStructuredOrg, List: values}, nil
	default:
		if err := validateScalarText(typ, raw); err != nil {
			return Value{}, err
		}
		return Value{Type: typ, Data: raw}, nil
	}
}

func parseStructuredComponents(raw string, names []string) ([]Component, error) {
	components := splitEscaped(raw, ';')
	if len(components) > len(names) {
		return nil, fmt.Errorf("vcard: expected at most %d components, got %d", len(names), len(components))
	}
	parts := make([]Component, len(names))
	for i, n := range names {
		var values []string
		if i < len(components) {
			items := splitEscaped(components[i], ',')
			values = make([]string, len(items))
			for j, it := range items {
				values[j] = unescapeText(it)
			}
		} else {
			values = []string{""}
		}
		parts[i] = NewComponent(n, ValueText, values...)
	}
	return parts, nil
}

func parseGender(raw string) (Value, error) {
	components := splitEscaped(raw, ';')
	if len(components) > 2 {
		return Value{}, errors.New("vcard: invalid GENDER value")
	}
	sex := unescapeText(components[0])
	switch sex {
	case "", "M", "F", "O", "N", "U":
	default:
		return Value{}, errors.New("vcard: invalid GENDER sex")
	}
	parts := []Component{NewComponent("sex", ValueText, sex)}
	if len(components) == 2 {
		parts = append(parts, NewComponent("identity", ValueText, unescapeText(components[1])))
	}
	return Value{Type: ValueStructuredGender, Parts: parts}, nil
}

func parseClientPIDMap(raw string) (Value, error) {
	components := splitEscaped(raw, ';')
	if len(components) != 2 {
		return Value{}, errors.New("vcard: invalid CLIENTPIDMAP value")
	}
	id := unescapeText(components[0])
	if !rePositiveInteger.MatchString(id) {
		return Value{}, errors.New("vcard: invalid CLIENTPIDMAP sourceid")
	}
	uri := unescapeText(components[1])
	if uri == "" {
		return Value{}, errors.New("vcard: invalid CLIENTPIDMAP uri")
	}
	parts := []Component{
		NewComponent("sourceid", ValueInteger, id),
		NewComponent("uri", ValueURI, uri),
	}
	return Value{Type: ValueStructuredClientPIDMap, Parts: parts}, nil
}

// Scalar shape validators.
var (
	reInteger         = regexp.MustCompile(`^[+-]?[0-9]+$`)
	rePositiveInteger = regexp.MustCompile(`^[1-9][0-9]*$`)
	reFloat           = regexp.MustCompile(`^[+-]?[0-9]+(\.[0-9]+)?$`)
	reBoolean         = regexp.MustCompile(`^(?i:true|false)$`)
	reUTCOffset       = regexp.MustCompile(`^[+-][0-9]{2}[0-9]{2}([0-9]{2})?$`)
	reLanguageTag     = regexp.MustCompile(`^(?i:[A-Za-z]{1,8}(-[A-Za-z0-9]{1,8})*)$`)

	reDateBody = regexp.MustCompile(`^(?:[0-9]{4}(?:[0-9]{2}(?:[0-9]{2})?)?|[0-9]{4}-[0-9]{2}(?:-[0-9]{2})?|--[0-9]{2}(?:-?[0-9]{2})?|---[0-9]{2})$`)

	reTimeBody = regexp.MustCompile(`^(?:[0-9]{2}(?:[0-9]{2}(?:[0-9]{2})?)?|-[0-9]{2}(?:[0-9]{2})?|--[0-9]{2})(?:[.,][0-9]+)?(?:Z|[+-][0-9]{2}(?:[0-9]{2})?)?$`)

	reTimestamp = regexp.MustCompile(`^[0-9]{8}T[0-9]{6}(?:[.,][0-9]+)?(?:Z|[+-][0-9]{2}(?:[0-9]{2})?)?$`)
)

func validateScalarText(typ, raw string) error {
	switch typ {
	case ValueURI, ValueText:
		return nil
	case ValueInteger:
		if !reInteger.MatchString(raw) {
			return errors.New("vcard: invalid integer value: " + raw)
		}
	case ValueFloat:
		if !reFloat.MatchString(raw) {
			return errors.New("vcard: invalid float value: " + raw)
		}
	case ValueBoolean:
		if !reBoolean.MatchString(raw) {
			return errors.New("vcard: invalid boolean value: " + raw)
		}
	case ValueUTCOffset:
		if !reUTCOffset.MatchString(raw) {
			return errors.New("vcard: invalid utc-offset value: " + raw)
		}
	case ValueLanguageTag:
		if raw == "" || !reLanguageTag.MatchString(raw) {
			return errors.New("vcard: invalid language-tag value: " + raw)
		}
	case ValueDate:
		if !reDateBody.MatchString(raw) {
			return errors.New("vcard: invalid date value: " + raw)
		}
	case ValueTime:
		s := strings.TrimPrefix(raw, "T")
		if !reTimeBody.MatchString(s) {
			return errors.New("vcard: invalid time value: " + raw)
		}
	case ValueDateTime:
		datePart, timePart, ok := splitDateTime(raw)
		if !ok || !reDateBody.MatchString(datePart) || !reTimeBody.MatchString(timePart) {
			return errors.New("vcard: invalid date-time value: " + raw)
		}
	case ValueTimestamp:
		if !reTimestamp.MatchString(raw) {
			return errors.New("vcard: invalid timestamp value: " + raw)
		}
	case ValueDateAndOrTime:
		if !validDateAndOrTime(raw) {
			return errors.New("vcard: invalid date-and-or-time value: " + raw)
		}
	default:
		return errors.New("vcard: unknown value type: " + typ)
	}
	return nil
}

func splitDateTime(raw string) (date, time string, ok bool) {
	i := strings.IndexByte(raw, 'T')
	if i < 0 {
		return "", "", false
	}
	return raw[:i], raw[i+1:], true
}

func validDateAndOrTime(raw string) bool {
	if reDateBody.MatchString(raw) || reTimestamp.MatchString(raw) || reTimeBody.MatchString(raw) {
		return true
	}
	if strings.HasPrefix(raw, "T") {
		return reTimeBody.MatchString(raw[1:])
	}
	datePart, timePart, ok := splitDateTime(raw)
	return ok && reDateBody.MatchString(datePart) && reTimeBody.MatchString(timePart)
}

// VCard4Encoder writes contacts as vCard 4 plain text.
type VCard4Encoder struct {
	w io.Writer
}

// NewVCard4Encoder creates a VCard4Encoder writing to w.
func NewVCard4Encoder(w io.Writer) *VCard4Encoder {
	return &VCard4Encoder{w: w}
}

// Encode writes one contact.
func (e *VCard4Encoder) Encode(c *Contact) error {
	var sb strings.Builder
	sb.WriteString("BEGIN:VCARD\r\n")
	sb.WriteString("VERSION:4.0\r\n")
	for _, p := range c.Properties {
		line, err := encodeContentLine(p)
		if err != nil {
			return err
		}
		sb.WriteString(line)
		sb.WriteString("\r\n")
	}
	sb.WriteString("END:VCARD\r\n")
	_, err := io.WriteString(e.w, sb.String())
	return err
}

type rawParam struct {
	Name   string
	Values []string
}

// encodePropertyRaw renders the canonical value and the logical parameter
// list (including a synthesized VALUE parameter when required).
func encodePropertyRaw(p *Property) (string, []rawParam, error) {
	value, err := encodePropertyValueRaw(p)
	if err != nil {
		return "", nil, err
	}

	var params []rawParam
	if vt := explicitValueToken(p); vt != "" {
		// VALUE parameter values are the registered (lower-case) type
		// tokens, e.g. ";VALUE=text:".
		params = append(params, rawParam{Name: ParamValue, Values: []string{vt}})
	}
	for _, pa := range p.Params {
		params = append(params, rawParam{Name: strings.ToUpper(pa.Name), Values: pa.Values})
	}
	return value, params, nil
}

// explicitValueToken reports the VALUE parameter that must be emitted on the
// text form, if any.
func explicitValueToken(p *Property) string {
	t := p.Value.Type
	if t == ValueXML || isStructuredType(t) {
		return ""
	}
	// Per RFC 6351 section 6, an <unknown> value converts directly and the
	// containing property must not carry a VALUE parameter.
	if t == ValueUnknown {
		return ""
	}
	def, known := defaultValueType(p.Name)
	if known {
		if t != def {
			return t
		}
		return ""
	}
	return t
}

func encodePropertyValueRaw(p *Property) (string, error) {
	v := p.Value
	switch v.Type {
	case ValueText, ValueUnknown:
		return escapeText(v.Data), nil
	case ValueTextList:
		parts := make([]string, len(v.List))
		for i, s := range v.List {
			parts[i] = escapeText(s)
		}
		return strings.Join(parts, ","), nil
	case ValueURI, ValueInteger, ValueFloat, ValueDate, ValueLanguageTag,
		ValueTimestamp:
		return v.Data, nil
	case ValueBoolean:
		if strings.EqualFold(v.Data, "true") {
			return "TRUE", nil
		}
		return "FALSE", nil
	case ValueUTCOffset:
		return v.Data, nil
	case ValueTime:
		s := v.Data
		if !strings.HasPrefix(s, "T") {
			s = "T" + s
		}
		return s, nil
	case ValueDateTime:
		return v.Data, nil
	case ValueDateAndOrTime:
		// A time-only date-and-or-time carries the "T" designator in text.
		s := v.Data
		if !strings.Contains(s, "T") && reTimeBody.MatchString(s) {
			s = "T" + s
		}
		return s, nil
	case ValueStructuredN, ValueStructuredADR:
		return encodeStructured(v.Parts), nil
	case ValueStructuredGender:
		sex := componentText(v.Parts, "sex")
		identity := componentTexts(v.Parts, "identity")
		raw := escapeText(firstOrEmpty(sex))
		if len(identity) > 0 {
			raw += ";" + strings.Join(escapeAll(identity), ",")
		}
		return raw, nil
	case ValueStructuredClientPIDMap:
		id := firstOrEmpty(componentTexts(v.Parts, "sourceid"))
		uri := firstOrEmpty(componentTexts(v.Parts, "uri"))
		return id + ";" + uri, nil
	case ValueStructuredOrg:
		parts := make([]string, len(v.List))
		for i, s := range v.List {
			parts[i] = escapeText(s)
		}
		return strings.Join(parts, ";"), nil
	case ValueXML:
		if v.XML == nil {
			return "", errors.New("vcard: missing XML node")
		}
		raw, err := serializeXMLNodeCompact(v.XML)
		if err != nil {
			return "", err
		}
		return escapeXMLPropertyValue(raw), nil
	default:
		return "", errors.New("vcard: unsupported value type: " + v.Type)
	}
}

func encodeStructured(parts []Component) string {
	var sb strings.Builder
	for i, part := range parts {
		if i > 0 {
			sb.WriteByte(';')
		}
		values := part.Values
		if len(values) == 0 {
			values = []string{""}
		}
		for j, s := range values {
			if j > 0 {
				sb.WriteByte(',')
			}
			sb.WriteString(escapeText(s))
		}
	}
	return sb.String()
}

func escapeAll(in []string) []string {
	out := make([]string, len(in))
	for i, s := range in {
		out[i] = escapeText(s)
	}
	return out
}

func componentTexts(parts []Component, name string) []string {
	for _, p := range parts {
		if p.Name == name {
			return p.Values
		}
	}
	return nil
}

func componentText(parts []Component, name string) []string {
	return componentTexts(parts, name)
}

func firstOrEmpty(s []string) string {
	if len(s) == 0 {
		return ""
	}
	return s[0]
}

func encodeContentLine(p *Property) (string, error) {
	value, params, err := encodePropertyRaw(p)
	if err != nil {
		return "", err
	}

	var sb strings.Builder
	if p.Group != "" {
		sb.WriteString(p.Group)
		sb.WriteByte('.')
	}
	sb.WriteString(strings.ToUpper(p.Name))
	for _, pa := range params {
		sb.WriteByte(';')
		sb.WriteString(pa.Name)
		sb.WriteByte('=')
		sb.WriteString(encodeParameterValue(pa.Values))
	}
	sb.WriteByte(':')
	sb.WriteString(value)
	return sb.String(), nil
}

// encodeParameterValue renders parameter values, quoting individual values
// when they contain characters not allowed in unquoted parameter text.
func encodeParameterValue(values []string) string {
	parts := make([]string, len(values))
	for i, v := range values {
		parts[i] = quoteParameterValue(v)
	}
	return strings.Join(parts, ",")
}

func quoteParameterValue(v string) string {
	safe := true
	for _, r := range v {
		if r == '"' || r == ';' || r == ':' || r == ',' || r < 0x20 {
			safe = false
			break
		}
	}
	if safe {
		return v
	}
	var sb strings.Builder
	sb.WriteByte('"')
	for _, r := range v {
		switch r {
		case '\\', '"':
			sb.WriteByte('\\')
			sb.WriteRune(r)
		case '\n':
			sb.WriteString(`\n`)
		case '\r':
			sb.WriteString(`\r`)
		default:
			sb.WriteRune(r)
		}
	}
	sb.WriteByte('"')
	return sb.String()
}
