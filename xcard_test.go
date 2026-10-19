package vcard

import (
	"bytes"
	"encoding/xml"
	"errors"
	"io"
	"reflect"
	"strings"
	"testing"
)

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func mustDecodeV4(t *testing.T, text string) *Contact {
	t.Helper()
	c, err := NewVCard4Decoder(strings.NewReader(text)).Decode()
	if err != nil {
		t.Fatalf("decode vcard:\n%v\ninput: %s", err, text)
	}
	return c
}

func encodeV4(t *testing.T, c *Contact) string {
	t.Helper()
	var buf bytes.Buffer
	if err := NewVCard4Encoder(&buf).Encode(c); err != nil {
		t.Fatalf("encode vcard: %v", err)
	}
	return buf.String()
}

func roundTripText(t *testing.T, text string) *Contact {
	t.Helper()
	c := mustDecodeV4(t, text)
	got := encodeV4(t, c)
	if got != text {
		t.Fatalf("text round trip mismatch\nwant %q\n got %q", text, got)
	}
	return c
}

func roundTripXMLText(t *testing.T, text string) string {
	t.Helper()
	c := mustDecodeV4(t, text)
	data, err := MarshalXCard([]*Contact{c})
	if err != nil {
		t.Fatalf("marshal xcard: %v", err)
	}
	cs, err := UnmarshalXCard(data)
	if err != nil {
		t.Fatalf("unmarshal xcard: %v\nxml:\n%s", err, data)
	}
	if len(cs) != 1 {
		t.Fatalf("expected 1 contact, got %d", len(cs))
	}
	got := encodeV4(t, cs[0])
	if got != text {
		t.Fatalf("text -> xml -> text mismatch\nwant %q\n got %q\nxml:\n%s", text, got, data)
	}
	return string(data)
}

// ---------------------------------------------------------------------------
// vCard 4 text <-> xCard round trips
// ---------------------------------------------------------------------------

func TestXCard_RichRoundTrip(t *testing.T) {
	text := "BEGIN:VCARD\r\n" +
		"VERSION:4.0\r\n" +
		"FN:J. Doe\r\n" +
		"N:Doe\\, Esq;John;Quin,Quincy;Mr.;Jr. III,Esq\r\n" +
		"NICKNAME:Jim,Jimmie\r\n" +
		"CATEGORIES:friends,colleagues\r\n" +
		"X-FILE;MEDIATYPE=image/jpeg:alien.jpg\r\n" +
		"X-WHO;VALUE=text:Bob\r\n" +
		"X-BOOL;VALUE=boolean:TRUE\r\n" +
		"X-INT;VALUE=integer:42\r\n" +
		"X-FLOAT;VALUE=float:3.14\r\n" +
		"X-DATE;VALUE=date:19850412\r\n" +
		"X-TIME;VALUE=time:T130500Z\r\n" +
		"X-DT;VALUE=date-time:19850412T101530+0200\r\n" +
		"X-TS;VALUE=timestamp:20080424T195243Z\r\n" +
		"X-LANG;VALUE=language-tag:en-US\r\n" +
		"GENDER:M;man\r\n" +
		"GENDER:O\r\n" +
		"GENDER:\r\n" +
		"GENDER:;non-binary\r\n" +
		"CLIENTPIDMAP:1;urn:uuid:53e374d9-337e-4727-8803-a1e9c14e0556\r\n" +
		"ORG:ABC\\, Inc.;Engineering;R&D\r\n" +
		"ADR;TYPE=home:;;123 Main St, A;Springfield;IL;62701;USA\r\n" +
		"TZ:-0500\r\n" +
		"TZ;VALUE=text:Eastern Time\r\n" +
		"TZ;VALUE=uri:/tz/nyc\r\n" +
		"BDAY;VALUE=text:circa 1800\r\n" +
		"BDAY:--0203\r\n" +
		"REV:20080424T195243Z\r\n" +
		"EMAIL;TYPE=work;PREF=1:jdoe@example.com\r\n" +
		"item1.TEL;TYPE=work,voice;X-CARRIER=\"a,b\":tel:+1-555\r\n" +
		"NOTE:line1\\nline2\\; with semis and a\\, comma\r\n" +
		"XML:<a xmlns=\"http://www.w3.org/1999/xhtml\" href=\"http://x?a=1&amp;b=2\">My page!</a>\r\n" +
		"END:VCARD\r\n"

	xmlOut := roundTripXMLText(t, text)

	// Standard shell and key elements.
	for _, want := range []string{
		`xmlns="urn:ietf:params:xml:ns:vcard-4.0"`,
		"<vcards ", "<vcard>",
		"<fn><text>J. Doe</text></fn>",
		"<surname>Doe, Esq</surname>",
		"<additional>Quin</additional>",
		"<additional>Quincy</additional>",
		"<suffix>Jr. III</suffix>",
		"<suffix>Esq</suffix>",
		"<x-file>", "<unknown>alien.jpg</unknown>",
		"<x-who><text>Bob</text></x-who>",
		"<boolean>true</boolean>",
		"<integer>42</integer>",
		"<float>3.14</float>",
		"<date>19850412</date>",
		"<time>130500Z</time>",
		"<date-time>19850412T101530+0200</date-time>",
		"<timestamp>20080424T195243Z</timestamp>",
		"<language-tag>en-US</language-tag>",
		"<sex>M</sex>", "<identity>man</identity>",
		"<sex>O</sex>", "<sex/>",
		"<sourceid>1</sourceid>",
		"<org>", "<text>ABC, Inc.</text>", "<text>R&amp;D</text>",
		"<utc-offset>-0500</utc-offset>",
		`<group name="item1">`,
		"<x-carrier>", "<unknown>a,b</unknown>",
		"<note><text>line1", "line2; with semis and a, comma</text></note>",
		"&amp;b=2",
	} {
		if !strings.Contains(xmlOut, want) {
			t.Errorf("xcard output missing %q\n%s", want, xmlOut)
		}
	}

	// The version is expressed only by the namespace.
	if strings.Contains(xmlOut, "<version>") {
		t.Errorf("xcard must not contain a version element:\n%s", xmlOut)
	}
}

func TestXCard_RFC6351Section6Example(t *testing.T) {
	// RFC 6351 section 6 equivalence example (text side).
	text := "BEGIN:VCARD\r\n" +
		"VERSION:4.0\r\n" +
		"FN:J. Doe\r\n" +
		"N:Doe;J.;;;\r\n" +
		"X-FILE;MEDIATYPE=image/jpeg:alien.jpg\r\n" +
		"XML:<a xmlns=\"http://www.w3.org/1999/xhtml\" href=\"http://www.example.com\">My web page!</a>\r\n" +
		"END:VCARD\r\n"

	c := mustDecodeV4(t, text)
	data, err := MarshalXCard([]*Contact{c})
	if err != nil {
		t.Fatal(err)
	}
	out := string(data)
	for _, want := range []string{
		"<fn><text>J. Doe</text></fn>",
		"<surname>Doe</surname>",
		"<given>J.</given>",
		"<additional/>", "<prefix/>", "<suffix/>",
		"<x-file>", "<mediatype>", "<text>image/jpeg</text>",
		"<unknown>alien.jpg</unknown>",
		`<a xmlns="http://www.w3.org/1999/xhtml" href="http://www.example.com">My web page!</a>`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
}

func TestXCard_ParseRFC6351AuthorExample(t *testing.T) {
	// Adapted from RFC 6351 section 4.
	xmlIn := `<?xml version="1.0" encoding="UTF-8"?>
<vcards xmlns="urn:ietf:params:xml:ns:vcard-4.0">
  <vcard>
    <fn><text>Simon Perreault</text></fn>
    <n>
      <surname>Perreault</surname>
      <given>Simon</given>
      <additional/>
      <prefix/>
      <suffix>ing. jr</suffix>
      <suffix>M.Sc.</suffix>
    </n>
    <bday><date>--0203</date></bday>
    <anniversary><date-time>20090808T1430-0500</date-time></anniversary>
    <gender><sex>M</sex></gender>
    <lang><parameters><pref><integer>1</integer></pref></parameters><language-tag>fr</language-tag></lang>
    <org><parameters><type><text>work</text></type></parameters><text>Viagenie</text></org>
    <adr>
      <parameters><type><text>work</text></type></parameters>
      <pobox/>
      <ext/>
      <street>2875 boul. Laurier, suite D2-630</street>
      <locality>Quebec</locality>
      <region>QC</region>
      <code>G1V 2M2</code>
      <country>Canada</country>
    </adr>
    <tel>
      <parameters><type><text>work</text><text>voice</text></type></parameters>
      <uri>tel:+1-418-656-9254;ext=102</uri>
    </tel>
    <tz><text>America/Montreal</text></tz>
  </vcard>
</vcards>`

	cs, err := UnmarshalXCard([]byte(xmlIn))
	if err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	c := cs[0]

	if got := c.Value("FN"); got != "Simon Perreault" {
		t.Errorf("FN = %q", got)
	}
	n := c.PropertiesByName("N")[0]
	if len(n.Value.Parts) != 5 {
		t.Fatalf("N has %d parts", len(n.Value.Parts))
	}
	if got := n.Value.Parts[4].Values; !reflect.DeepEqual(got, []string{"ing. jr", "M.Sc."}) {
		t.Errorf("suffix values = %v", got)
	}
	if got := c.Value("BDAY"); got != "--0203" {
		t.Errorf("BDAY = %q", got)
	}
	if got := c.Value("ANNIVERSARY"); got != "20090808T1430-0500" {
		t.Errorf("ANNIVERSARY = %q", got)
	}
	if got := c.Value("TZ"); got != "America/Montreal" {
		t.Errorf("TZ = %q", got)
	}
	if got := c.Value("TEL"); got != "tel:+1-418-656-9254;ext=102" {
		t.Errorf("TEL = %q", got)
	}

	// The tel URI contains a semicolon that must survive untouched.
	text := encodeV4(t, c)
	if !strings.Contains(text, "TEL;TYPE=work,voice:tel:+1-418-656-9254;ext=102\r\n") {
		t.Errorf("tel uri mangled:\n%s", text)
	}
	// Version restored on the text side.
	if !strings.Contains(text, "VERSION:4.0\r\n") {
		t.Errorf("version not restored:\n%s", text)
	}
	// ADR empty leading components restored.
	if !strings.Contains(text, "ADR;TYPE=work:;;2875 boul.") {
		t.Errorf("adr structure lost:\n%s", text)
	}
}

// Contact.Value is a small test helper for scalar properties.
func (c *Contact) Value(name string) string {
	for _, p := range c.Properties {
		if strings.EqualFold(p.Name, name) {
			return p.Value.Data
		}
	}
	return ""
}

func TestXCard_MultipleContacts(t *testing.T) {
	c1 := NewContact()
	c1.Add(NewProperty("", "FN", nil, TextValue("One")))
	c2 := NewContact()
	c2.Add(NewProperty("", "FN", nil, TextValue("Two")))

	data, err := MarshalXCard([]*Contact{c1, c2})
	if err != nil {
		t.Fatal(err)
	}
	cs, err := UnmarshalXCard(data)
	if err != nil {
		t.Fatal(err)
	}
	if len(cs) != 2 || cs[0].Value("FN") != "One" || cs[1].Value("FN") != "Two" {
		t.Fatalf("unexpected contacts: %v", cs)
	}

	// Streaming decoder.
	dec := NewXCardDecoder(bytes.NewReader(data))
	var n int
	for {
		_, err := dec.Decode()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		n++
	}
	if n != 2 {
		t.Errorf("streaming decode got %d contacts", n)
	}
}

func TestXCard_Groups(t *testing.T) {
	text := "BEGIN:VCARD\r\n" +
		"VERSION:4.0\r\n" +
		"item1.URL:http://example.com\r\n" +
		"item1.X-ABLABEL:Home\r\n" +
		"NOTE:not grouped\r\n" +
		"item1.TEL:tel:1\r\n" +
		"END:VCARD\r\n"

	out := roundTripXMLText(t, text)
	if strings.Count(out, `<group name="item1">`) != 2 {
		t.Errorf("expected two non-adjacent group wrappers:\n%s", out)
	}
}

// ---------------------------------------------------------------------------
// Escaping / structural delimiters
// ---------------------------------------------------------------------------

func TestXCard_StructuredDelimiters(t *testing.T) {
	text := "BEGIN:VCARD\r\n" +
		"VERSION:4.0\r\n" +
		"N:a\\;b;c\\,d;e,f;g;h\r\n" +
		"ADR:box\\;one;ext;street,2nd;loc;reg;code;c\\,try\r\n" +
		"END:VCARD\r\n"
	c := roundTripText(t, text)

	n := c.PropertiesByName("N")[0].Value.Parts
	// Literal semicolon preserved inside surname.
	if got := n[0].Values[0]; got != "a;b" {
		t.Errorf("surname = %q", got)
	}
	// Escaped comma stays inside given; unescaped comma splits additional.
	if got := n[1].Values; !reflect.DeepEqual(got, []string{"c,d"}) {
		t.Errorf("given = %v", got)
	}
	if got := n[2].Values; !reflect.DeepEqual(got, []string{"e", "f"}) {
		t.Errorf("additional = %v", got)
	}

	adr := c.PropertiesByName("ADR")[0].Value.Parts
	if got := adr[0].Values[0]; got != "box;one" {
		t.Errorf("pobox = %q", got)
	}
	if got := adr[2].Values; !reflect.DeepEqual(got, []string{"street", "2nd"}) {
		t.Errorf("street = %v", got)
	}
	if got := adr[6].Values[0]; got != "c,try" {
		t.Errorf("country = %q", got)
	}
}

func TestXCard_XMLEscaping(t *testing.T) {
	text := "BEGIN:VCARD\r\n" +
		"VERSION:4.0\r\n" +
		"NOTE:5 < 6 & 7 > 4\r\n" +
		"XML:<b xmlns=\"urn:x\" x:id=\"1\" xmlns:x=\"urn:y\"><c>text &amp; more</c></b>\r\n" +
		"END:VCARD\r\n"
	c := mustDecodeV4(t, text)
	data, err := MarshalXCard([]*Contact{c})
	if err != nil {
		t.Fatal(err)
	}
	xmlOut := string(data)
	if !strings.Contains(xmlOut, "5 &lt; 6 &amp; 7 &gt; 4") {
		t.Errorf("text not XML-escaped:\n%s", xmlOut)
	}

	// Expanded name and namespaced attribute preserved.
	cs, err := UnmarshalXCard(data)
	if err != nil {
		t.Fatal(err)
	}
	var node *XMLNode
	for _, p := range cs[0].Properties {
		if p.Name == FieldXML {
			node = p.Value.XML
		}
	}
	if node == nil {
		t.Fatal("missing XML property")
	}
	if node.Name.Space != "urn:x" || node.Name.Local != "b" {
		t.Errorf("expanded name = %v", node.Name)
	}
	var idAttr *xml.Attr
	for i := range node.Attr {
		if node.Attr[i].Name.Local == "id" {
			idAttr = &node.Attr[i]
		}
	}
	if idAttr == nil || idAttr.Name.Space != "urn:y" || idAttr.Value != "1" {
		t.Errorf("namespaced attribute not preserved: %+v", node.Attr)
	}

	// Back to text: XML prefixes may be re-generated, but expanded names
	// and content meaning survive.
	gotText := encodeV4(t, cs[0])
	for _, want := range []string{
		"NOTE:5 < 6 & 7 > 4",
		`<b xmlns="urn:x"`,
		`:id="1"`,
		`="urn:y"`,
		"<c>text &amp; more</c>",
	} {
		if !strings.Contains(gotText, want) {
			t.Errorf("text output missing %q:\n%s", want, gotText)
		}
	}

	// And the model is stable across another round trip.
	data2, err := MarshalXCard(cs)
	if err != nil {
		t.Fatal(err)
	}
	cs2, err := UnmarshalXCard(data2)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(cs2[0], cs[0]) {
		t.Fatal("second XML round trip changed the model")
	}
}

func TestXCard_ParameterQuoting(t *testing.T) {
	// RFC 6351 section 6 parameter example.
	xmlIn := `<vcards xmlns="urn:ietf:params:xml:ns:vcard-4.0"><vcard>` +
		`<x-p><parameters><x-param><unknown>"foo","bar"</unknown></x-param></parameters><unknown>v</unknown></x-p>` +
		`</vcard></vcards>`
	cs, err := UnmarshalXCard([]byte(xmlIn))
	if err != nil {
		t.Fatal(err)
	}
	text := encodeV4(t, cs[0])
	if !strings.Contains(text, `X-PARAM="\"foo\",\"bar\""`) {
		t.Errorf("parameter quoting wrong:\n%s", text)
	}
	// Round trip back to XML.
	roundTripXMLText(t, text)
}

// ---------------------------------------------------------------------------
// Folding and fragmented input
// ---------------------------------------------------------------------------

func TestXCard_FoldingAndFragments(t *testing.T) {
	contiguous := "BEGIN:VCARD\r\nVERSION:4.0\r\nNOTE:a long note\r\nEND:VCARD\r\n"
	folded := "BEGIN:VCARD\r\nVERSION:4.0\r\nNOTE:a long\r\n  note\r\nEND:VCARD\r\n"

	c1 := mustDecodeV4(t, contiguous)
	c2 := mustDecodeV4(t, folded)
	if !reflect.DeepEqual(c1, c2) {
		t.Fatal("folded and contiguous inputs differ")
	}

	// Byte-by-byte delivery must give the same contacts.
	slow := &slowReader{data: []byte(contiguous)}
	c3, err := NewVCard4Decoder(slow).Decode()
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(c1, c3) {
		t.Fatal("fragmented text input differs")
	}

	data, err := MarshalXCard([]*Contact{c1})
	if err != nil {
		t.Fatal(err)
	}
	c4, err := NewXCardDecoder(&slowReader{data: data}).Decode()
	if err != nil {
		t.Fatal(err)
	}
	if c4.Value("NOTE") != "a long note" {
		t.Errorf("fragmented xml parse gave %q", c4.Value("NOTE"))
	}
}

type slowReader struct {
	data []byte
}

func (r *slowReader) Read(p []byte) (int, error) {
	if len(r.data) == 0 {
		return 0, io.EOF
	}
	p[0] = r.data[0]
	r.data = r.data[1:]
	return 1, nil
}

// ---------------------------------------------------------------------------
// Error handling
// ---------------------------------------------------------------------------

func TestXCard_InvalidInputs(t *testing.T) {
	badText := []string{
		"",
		"VERSION:4.0\r\nEND:VCARD\r\n",
		"BEGIN:VCARD\r\nVERSION:4.0\r\n",
		"BEGIN:VCARD\r\nVERSION:3.0\r\nEND:VCARD\r\n",
		"BEGIN:VCARD\r\nVERSION:4.0\r\nN:a;b;c;d;e;f\r\nEND:VCARD\r\n",
		"BEGIN:VCARD\r\nVERSION:4.0\r\nGENDER:X\r\nEND:VCARD\r\n",
		"BEGIN:VCARD\r\nVERSION:4.0\r\nCLIENTPIDMAP:a;urn:x\r\nEND:VCARD\r\n",
		"BEGIN:VCARD\r\nVERSION:4.0\r\nCLIENTPIDMAP:1\r\nEND:VCARD\r\n",
		"BEGIN:VCARD\r\nVERSION:4.0\r\nX-N;VALUE=nonsense:x\r\nEND:VCARD\r\n",
		"BEGIN:VCARD\r\nVERSION:4.0\r\nX-N;VALUE=integer:abc\r\nEND:VCARD\r\n",
		"BEGIN:VCARD\r\nVERSION:4.0\r\nX-N;VALUE=time:noon\r\nEND:VCARD\r\n",
		"BEGIN:VCARD\r\nVERSION:4.0\r\nBADPARAM;X:v\r\nEND:VCARD\r\n",
		"BEGIN:VCARD\r\nVERSION:4.0\r\nBADPARAM;X=\"unterminated:v\r\nEND:VCARD\r\n",
	}
	for _, in := range badText {
		if _, err := NewVCard4Decoder(strings.NewReader(in)).Decode(); err == nil {
			t.Errorf("expected error for text input: %q", in)
		}
	}

	const ns = "urn:ietf:params:xml:ns:vcard-4.0"
	badXML := []string{
		``,
		`<?xml version="1.0"?><foo xmlns="` + ns + `"/>`,
		`<vcards xmlns="` + ns + `"></vcards>`,
		`<vcards xmlns="urn:other"><vcard/></vcards>`,
		`<vcards xmlns="` + ns + `"><vcard><fn/></vcard></vcards>`,
		`<vcards xmlns="` + ns + `"><vcard><fn><integer>x</integer></fn></vcard></vcards>`,
		`<vcards xmlns="` + ns + `"><vcard><gender><sex>X</sex></gender></vcard></vcards>`,
		`<vcards xmlns="` + ns + `"><vcard><gender/></vcard></vcards>`,
		`<vcards xmlns="` + ns + `"><vcard><clientpidmap><sourceid>0</sourceid><uri>x</uri></clientpidmap></vcard></vcards>`,
		`<vcards xmlns="` + ns + `"><vcard><clientpidmap><sourceid>1</sourceid></clientpidmap></vcard></vcards>`,
		`<vcards xmlns="` + ns + `"><vcard><version>4.0</version></vcard></vcards>`,
		`<vcards xmlns="` + ns + `"><vcard><group><fn><text>x</text></fn></group></vcard></vcards>`,
		`<!DOCTYPE vcards [<!ENTITY x "boom">]><vcards xmlns="` + ns + `"><vcard/></vcards>`,
		`<vcards xmlns="` + ns + `"><vcard><fn><text>trunc`,
	}
	for _, in := range badXML {
		if _, err := UnmarshalXCard([]byte(in)); err == nil {
			t.Errorf("expected error for xml input: %q", in)
		}
	}
}

func TestXCard_NoCrashesOnGarbage(t *testing.T) {
	const ns = "urn:ietf:params:xml:ns:vcard-4.0"
	garbage := []string{
		"<", "</", "<vcards", "<vcards>", "<vcards/>",
		"<vcards xmlns>", `<vcards xmlns=""><vcard>`,
		`<vcards xmlns="` + ns + `"><vcard><n>`,
	}
	for _, in := range garbage {
		func() {
			defer func() {
				if r := recover(); r != nil {
					t.Errorf("panic on %q: %v", in, r)
				}
			}()
			_, _ = UnmarshalXCard([]byte(in))
		}()
	}

	// Illegal recognized parameter shape is a plain error.
	bad := `<vcards xmlns="` + ns + `"><vcard>` +
		`<x-a><parameters><pref><integer>x</integer></pref></parameters><unknown>v</unknown></x-a>` +
		`</vcard></vcards>`
	if _, err := UnmarshalXCard([]byte(bad)); err == nil {
		t.Errorf("expected error for malformed pref parameter")
	}
}

// ---------------------------------------------------------------------------
// Independence and non-mutation
// ---------------------------------------------------------------------------

func TestXCard_IndependenceAfterError(t *testing.T) {
	if _, err := UnmarshalXCard([]byte("not xml")); err == nil {
		t.Fatal("expected error")
	}
	c, err := UnmarshalXCard([]byte(`<vcards xmlns="urn:ietf:params:xml:ns:vcard-4.0"><vcard><fn><text>ok</text></fn></vcard></vcards>`))
	if err != nil || c[0].Value("FN") != "ok" {
		t.Fatalf("valid operation after rejected one failed: %v %v", c, err)
	}

	// Invalid text contact does not poison the next decode.
	r := strings.NewReader("garbage\r\nBEGIN:VCARD\r\nVERSION:4.0\r\nFN:ok\r\nEND:VCARD\r\n")
	dec := NewVCard4Decoder(r)
	if _, err := dec.Decode(); err == nil {
		t.Fatal("expected error")
	}
	c2 := NewContact()
	c2.Add(NewProperty("", "FN", nil, TextValue("ok")))
	if err := NewVCard4Encoder(&bytes.Buffer{}).Encode(c2); err != nil {
		t.Fatalf("encode after error failed: %v", err)
	}
}

func TestXCard_DoesNotMutateContact(t *testing.T) {
	c := NewContact()
	c.Add(NewProperty("g1", "X-FLAG", nil, ScalarValue(ValueBoolean, "TRUE")))
	c.Add(NewProperty("g1", "X-T", nil, ScalarValue(ValueTime, "T120000Z")))
	want := cloneContact(c)

	var buf bytes.Buffer
	if err := NewXCardEncoder(&buf).Encode(c); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(c, want) {
		t.Fatalf("XCard Encode mutated contact:\nwant %+v\ngot  %+v", want, c)
	}
	if err := NewVCard4Encoder(&bytes.Buffer{}).Encode(c); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(c, want) {
		t.Fatalf("VCard4 Encode mutated contact")
	}
}

func cloneContact(c *Contact) *Contact {
	out := NewContact()
	for _, p := range c.Properties {
		np := &Property{Group: p.Group, Name: p.Name}
		for _, pa := range p.Params {
			np.Params = append(np.Params, Parameter{pa.Name, pa.Type, append([]string(nil), pa.Values...)})
		}
		v := p.Value
		np.Value = Value{Type: v.Type, Data: v.Data, List: append([]string(nil), v.List...)}
		for _, comp := range v.Parts {
			np.Value.Parts = append(np.Value.Parts, Component{comp.Name, comp.Type, append([]string(nil), comp.Values...)})
		}
		out.Add(np)
	}
	return out
}

// ---------------------------------------------------------------------------
// I/O failures
// ---------------------------------------------------------------------------

type failWriter struct{ after int }

func (w *failWriter) Write(p []byte) (int, error) {
	if w.after <= 0 {
		return 0, errors.New("disk full")
	}
	w.after--
	return len(p), nil
}

func TestXCard_WriterFailure(t *testing.T) {
	c, _ := UnmarshalXCard([]byte(`<vcards xmlns="urn:ietf:params:xml:ns:vcard-4.0"><vcard><fn><text>ok</text></fn></vcard></vcards>`))
	if err := NewXCardEncoder(&failWriter{}).Encode(c[0]); err == nil {
		t.Error("expected write failure")
	}
	if err := NewVCard4Encoder(&failWriter{}).Encode(NewContact()); err == nil {
		t.Error("expected write failure")
	}
}

func TestXCard_ReaderFailure(t *testing.T) {
	dec := NewXCardDecoder(&errReader{})
	if _, err := dec.Decode(); err == nil {
		t.Error("expected read failure")
	}
}

type errReader struct{}

func (errReader) Read(p []byte) (int, error) { return 0, errors.New("read failed") }

// ---------------------------------------------------------------------------
// Card bridge and coexistence with the legacy codec
// ---------------------------------------------------------------------------

func TestXCard_CardBridge(t *testing.T) {
	card := make(Card)
	card.Set(FieldVersion, &Field{Value: "4.0"})
	card.Set(FieldFormattedName, &Field{Value: "J. Doe"})
	card.Set(FieldName, &Field{Value: "Doe;John;;Mr.;"})
	card.Add(FieldEmail, &Field{Value: "j@example.com", Params: Params{"TYPE": {"work"}, "PREF": {"1"}}})
	card.Add(FieldClientPIDMap, &Field{Value: "1;urn:uuid:53e374d9"})

	contact, err := ContactFromCard(card)
	if err != nil {
		t.Fatal(err)
	}
	data, err := MarshalXCard([]*Contact{contact})
	if err != nil {
		t.Fatal(err)
	}
	cs, err := UnmarshalXCard(data)
	if err != nil {
		t.Fatal(err)
	}
	back, err := cs[0].Card()
	if err != nil {
		t.Fatal(err)
	}
	if got := back.Value(FieldFormattedName); got != "J. Doe" {
		t.Errorf("FN = %q", got)
	}
	name := back.Name()
	if name == nil || name.FamilyName != "Doe" || name.GivenName != "John" || name.HonorificPrefix != "Mr." {
		t.Errorf("name = %+v", name)
	}
	if got := back.PreferredValue(FieldEmail); got != "j@example.com" {
		t.Errorf("email = %q", got)
	}

	// The legacy encoder must emit the CLIENTPIDMAP structure intact.
	var buf bytes.Buffer
	if err := NewEncoder(&buf).Encode(back); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(buf.String(), "CLIENTPIDMAP:1;urn:uuid:53e374d9\r\n") {
		t.Errorf("clientpidmap lost through legacy encoder:\n%s", buf.String())
	}
}

func TestXCard_BridgeDoesNotChangeInputCard(t *testing.T) {
	card := testCard
	_, err := ContactFromCard(card)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(card, testCard) {
		t.Error("ContactFromCard mutated the card")
	}
}

func TestXCard_PublicConstructors(t *testing.T) {
	c := NewContact()
	c.Add(NewProperty("", FieldFormattedName, nil, TextValue("Jane")))
	c.Add(NewProperty("", FieldName, nil, StructuredValue(ValueStructuredN, []Component{
		NewComponent("surname", ValueText, "Doe"),
		NewComponent("given", ValueText, "Jane"),
		NewComponent("additional", ValueText, ""),
		NewComponent("prefix", ValueText, ""),
		NewComponent("suffix", ValueText, ""),
	})))
	c.Add(NewProperty("", FieldCategories, nil, TextListValue([]string{"a", "b"})))
	c.Add(NewProperty("", "X-P", []Parameter{TextParameter("X-N", "v1", "v2")}, TextValue("x")))

	xmlNode := NewXMLNode("urn:example", "ext", xml.Attr{Name: xml.Name{Local: "k"}, Value: "v"})
	xmlNode.AppendText("hi")
	c.Add(NewProperty("", FieldXML, nil, ForeignValue(xmlNode)))

	data, err := MarshalXCard([]*Contact{c})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{
		"<fn><text>Jane</text></fn>",
		"<categories><text>a</text><text>b</text></categories>",
		`<x-n>`, `<text>v1</text>`, `<text>v2</text>`,
		`<ext xmlns="urn:example" k="v">hi</ext>`,
	}
	for _, w := range want {
		if !strings.Contains(string(data), w) {
			t.Errorf("missing %q in\n%s", w, data)
		}
	}
	roundTripXMLText(t, encodeV4(t, c))
}

// ---------------------------------------------------------------------------
// Unknown extensions: ignored descendants and parameters
// ---------------------------------------------------------------------------

func TestXCard_UnknownContentIgnored(t *testing.T) {
	xmlIn := `<vcards xmlns="urn:ietf:params:xml:ns:vcard-4.0" xmlns:e="urn:ext">
  <vcard>
    <fn e:foo="bar" ><text>Jane</text><e:weird>drop</e:weird></fn>
    <e:future><text>x</text></e:future>
    <frob><text>ignored</text></frob>
  </vcard>
</vcards>`
	cs, err := UnmarshalXCard([]byte(xmlIn))
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, p := range cs[0].Properties {
		names = append(names, p.Name)
	}
	// FN is kept; the foreign-namespaced element becomes an XML property;
	// the vcard-namespace <frob> is ignored per RFC 6351 section 5.1.
	if len(names) != 2 || names[0] != "FN" || names[1] != "XML" {
		t.Fatalf("unexpected properties: %v", names)
	}
	if cs[0].Properties[1].Value.XML.Name.Local != "future" {
		t.Fatalf("foreign element not preserved: %+v", cs[0].Properties[1].Value.XML.Name)
	}

	// Unknown parameter element types: <unknown> behaves like text.
	xmlIn2 := `<vcards xmlns="urn:ietf:params:xml:ns:vcard-4.0"><vcard>` +
		`<x-a><parameters><x-q><unknown>v1</unknown><unknown>v2</unknown></x-q></parameters><unknown>raw</unknown></x-a>` +
		`</vcard></vcards>`
	cs2, err := UnmarshalXCard([]byte(xmlIn2))
	if err != nil {
		t.Fatal(err)
	}
	text := encodeV4(t, cs2[0])
	if !strings.Contains(text, "X-Q=v1,v2:") {
		t.Errorf("unknown parameter not treated as text list:\n%s", text)
	}
}
