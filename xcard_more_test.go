package vcard

import (
	"bytes"
	"io"
	"reflect"
	"strings"
	"testing"
)

func TestVCard4_StreamMultiple(t *testing.T) {
	in := "BEGIN:VCARD\r\nVERSION:4.0\r\nFN:One\r\nEND:VCARD\r\n" +
		"BEGIN:VCARD\r\nVERSION:4.0\r\nFN:Two\r\nEND:VCARD\r\n"

	dec := NewVCard4Decoder(strings.NewReader(in))
	var names []string
	for {
		c, err := dec.Decode()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		names = append(names, c.Value("FN"))
	}
	if !reflect.DeepEqual(names, []string{"One", "Two"}) {
		t.Fatalf("names = %v", names)
	}

	// Byte-by-byte delivery gives identical results.
	slow := NewVCard4Decoder(&slowReader{data: []byte(in)})
	c1, err := slow.Decode()
	if err != nil {
		t.Fatal(err)
	}
	if c1.Value("FN") != "One" {
		t.Fatalf("first slow card = %v", c1)
	}
	c2, err := slow.Decode()
	if err != nil {
		t.Fatal(err)
	}
	if c2.Value("FN") != "Two" {
		t.Fatalf("second slow card = %v", c2)
	}
	if _, err := slow.Decode(); err != io.EOF {
		t.Fatalf("expected EOF, got %v", err)
	}
}

func TestXCard_StreamingEncoder(t *testing.T) {
	var buf bytes.Buffer
	enc := NewXCardEncoder(&buf)
	for _, name := range []string{"One", "Two"} {
		c := NewContact()
		c.Add(NewProperty("", "FN", nil, TextValue(name)))
		if err := enc.Encode(c); err != nil {
			t.Fatal(err)
		}
	}
	if err := enc.Close(); err != nil {
		t.Fatal(err)
	}
	cs, err := UnmarshalXCard(buf.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	if len(cs) != 2 || cs[0].Value("FN") != "One" || cs[1].Value("FN") != "Two" {
		t.Fatalf("streaming encoder produced %v", cs)
	}
}

func TestXCard_TimeOnlyDateAndOrTime(t *testing.T) {
	text := "BEGIN:VCARD\r\nVERSION:4.0\r\nX-T;VALUE=date-and-or-time:T130500Z\r\nEND:VCARD\r\n"
	xmlOut := roundTripXMLText(t, text)
	if !strings.Contains(xmlOut, "<date-and-or-time>130500Z</date-and-or-time>") {
		t.Fatalf("time-only date-and-or-time wrong:\n%s", xmlOut)
	}
}

func TestXCard_LegacyCodecInterop(t *testing.T) {
	// A card produced by the new model must be readable by the legacy text
	// codec and survive another pass through it.
	c := NewContact()
	c.Add(NewProperty("", "FN", nil, TextValue("Jane Doe")))
	c.Add(NewProperty("", "N", nil, StructuredValue(ValueStructuredN, []Component{
		NewComponent("surname", ValueText, "Doe"),
		NewComponent("given", ValueText, "Jane"),
		NewComponent("additional", ValueText, ""),
		NewComponent("prefix", ValueText, ""),
		NewComponent("suffix", ValueText, ""),
	})))
	c.Add(NewProperty("", "EMAIL", []Parameter{TextParameter("TYPE", "work")}, TextValue("jane@example.com")))
	c.Add(NewProperty("", "CLIENTPIDMAP", nil, StructuredValue(ValueStructuredClientPIDMap, []Component{
		NewComponent("sourceid", ValueInteger, "2"),
		NewComponent("uri", ValueURI, "urn:uuid:x"),
	})))

	var buf bytes.Buffer
	if err := NewVCard4Encoder(&buf).Encode(c); err != nil {
		t.Fatal(err)
	}
	legacyCard, err := NewDecoder(&buf).Decode()
	if err != nil {
		t.Fatalf("legacy decoder rejected output: %v\n%s", err, buf.String())
	}
	if got := legacyCard.Value(FieldFormattedName); got != "Jane Doe" {
		t.Errorf("FN = %q", got)
	}
	name := legacyCard.Name()
	if name == nil || name.FamilyName != "Doe" || name.GivenName != "Jane" {
		t.Errorf("N = %+v", name)
	}
	if got := legacyCard.Value(FieldClientPIDMap); got != "2;urn:uuid:x" {
		t.Errorf("CLIENTPIDMAP = %q", got)
	}

	// And the legacy encoder must be able to re-emit it without data loss
	// (property ordering is not part of the preserved data).
	var buf2 bytes.Buffer
	if err := NewEncoder(&buf2).Encode(legacyCard); err != nil {
		t.Fatal(err)
	}
	c2 := mustDecodeV4(t, buf2.String())
	if !contactsEqual(c, c2) {
		t.Fatalf("legacy round trip changed data\nwant %+v\ngot  %+v", c, c2)
	}
}

// contactsEqual compares two contacts ignoring cross-property ordering.
func contactsEqual(a, b *Contact) bool {
	if len(a.Properties) != len(b.Properties) {
		return false
	}
	lines := func(c *Contact) map[string]int {
		m := map[string]int{}
		for _, p := range c.Properties {
			line, err := encodeContentLine(p)
			if err != nil {
				panic(err)
			}
			m[line]++
		}
		return m
	}
	return reflect.DeepEqual(lines(a), lines(b))
}

func TestXCard_EmptyStructuredValues(t *testing.T) {
	// Missing components are padded to the required canonical separators.
	text := "BEGIN:VCARD\r\nVERSION:4.0\r\n" +
		"N:;;;;\r\nADR:;;;;;;\r\nORG:\r\nNICKNAME:\r\nCLIENTPIDMAP:1;urn:x\r\n" +
		"END:VCARD\r\n"
	// The short form carries the same data.
	short := "BEGIN:VCARD\r\nVERSION:4.0\r\n" +
		"N:\r\nADR:\r\nORG:\r\nNICKNAME:\r\nCLIENTPIDMAP:1;urn:x\r\n" +
		"END:VCARD\r\n"
	c1 := mustDecodeV4(t, text)
	c2 := mustDecodeV4(t, short)
	if !contactsEqual(c1, c2) {
		t.Fatal("N: and N:;;;; decode differently")
	}
	xmlOut := roundTripXMLText(t, text)
	for _, want := range []string{
		"<surname/>", "<given/>", "<additional/>", "<prefix/>", "<suffix/>",
		"<pobox/>", "<ext/>", "<street/>", "<locality/>", "<region/>", "<code/>", "<country/>",
		"<org><text/></org>", "<nickname><text/></nickname>",
	} {
		if !strings.Contains(xmlOut, want) {
			t.Errorf("missing %q in:\n%s", want, xmlOut)
		}
	}
}

func TestXCard_GroupedForeignXML(t *testing.T) {
	xmlIn := `<vcards xmlns="urn:ietf:params:xml:ns:vcard-4.0"><vcard>` +
		`<group name="g1"><ext xmlns="urn:example">hi</ext><fn><text>x</text></fn></group>` +
		`</vcard></vcards>`
	cs, err := UnmarshalXCard([]byte(xmlIn))
	if err != nil {
		t.Fatal(err)
	}
	var groups []string
	for _, p := range cs[0].Properties {
		groups = append(groups, p.Group+"/"+p.Name)
	}
	if got := strings.Join(groups, ","); got != "g1/XML,g1/FN" {
		t.Fatalf("properties = %q", got)
	}
	data, err := MarshalXCard(cs)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `<group name="g1">`) ||
		!strings.Contains(string(data), `<ext xmlns="urn:example">hi</ext>`) {
		t.Fatalf("grouped foreign xml lost:\n%s", data)
	}
	// And back to text.
	c2, err := UnmarshalXCard(data)
	if err != nil {
		t.Fatal(err)
	}
	txt := encodeV4(t, c2[0])
	if !strings.Contains(txt, "g1.XML:<ext xmlns=\"urn:example\">hi</ext>") {
		t.Fatalf("text form wrong:\n%s", txt)
	}
}

func TestXCard_ExplicitValueOnStructuredRejected(t *testing.T) {
	bad := []string{
		"BEGIN:VCARD\r\nVERSION:4.0\r\nN;VALUE=text:a\r\nEND:VCARD\r\n",
		"BEGIN:VCARD\r\nVERSION:4.0\r\nADR;VALUE=text:a\r\nEND:VCARD\r\n",
		"BEGIN:VCARD\r\nVERSION:4.0\r\nVERSION;VALUE=uri:x\r\nEND:VCARD\r\n",
		"BEGIN:VCARD\r\nVERSION:4.0\r\nXML;ALTID=1:<x xmlns=\"urn:x\"/>\r\nEND:VCARD\r\n",
		"BEGIN:VCARD\r\nVERSION:4.0\r\nXML:<vcards xmlns=\"urn:ietf:params:xml:ns:vcard-4.0\"/>\r\nEND:VCARD\r\n",
	}
	for _, in := range bad {
		if _, err := NewVCard4Decoder(strings.NewReader(in)).Decode(); err == nil {
			t.Errorf("expected error for %q", in)
		}
	}
}

func TestXCard_BooleanNormalization(t *testing.T) {
	// TRUE/FALSE text and true/false XML mean the same boolean.
	text := "BEGIN:VCARD\r\nVERSION:4.0\r\nX-F;VALUE=boolean:FALSE\r\nEND:VCARD\r\n"
	c := mustDecodeV4(t, text)
	data, err := MarshalXCard([]*Contact{c})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "<boolean>false</boolean>") {
		t.Fatalf("boolean not normalized in XML: %s", data)
	}
	cs, err := UnmarshalXCard([]byte(`<vcards xmlns="urn:ietf:params:xml:ns:vcard-4.0"><vcard><x-f><boolean>1</boolean></x-f></vcard></vcards>`))
	if err != nil {
		t.Fatal(err)
	}
	if got := encodeV4(t, cs[0]); !strings.Contains(got, "X-F;VALUE=boolean:TRUE") {
		t.Fatalf("boolean 1 not mapped to TRUE: %s", got)
	}
}
