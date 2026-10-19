package vcard_test

import (
	"bytes"
	"fmt"
	"io"
	"log"
	"strings"

	"github.com/emersion/go-vcard"
)

// MarshalXCard converts lossless contacts to an RFC 6351 xCard document.
func ExampleMarshalXCard() {
	contact := vcard.NewContact()
	contact.Add(vcard.NewProperty("", vcard.FieldFormattedName, nil, vcard.TextValue("Jane Doe")))

	data, err := vcard.MarshalXCard([]*vcard.Contact{contact})
	if err != nil {
		log.Fatal(err)
	}
	fmt.Print(string(data))
	// Output:
	// <?xml version="1.0" encoding="UTF-8"?>
	// <vcards xmlns="urn:ietf:params:xml:ns:vcard-4.0">
	//   <vcard>
	//     <fn><text>Jane Doe</text></fn>
	//   </vcard>
	// </vcards>
}

// Convert a vCard 4 text stream to xCard using the streaming codecs.
func ExampleNewXCardEncoder_textToXML() {
	text := "BEGIN:VCARD\r\nVERSION:4.0\r\nFN:Jane Doe\r\nEND:VCARD\r\n"

	var out bytes.Buffer
	xmlEnc := vcard.NewXCardEncoder(&out)
	textDec := vcard.NewVCard4Decoder(strings.NewReader(text))
	for {
		contact, err := textDec.Decode()
		if err == io.EOF {
			break
		}
		if err != nil {
			log.Fatal(err)
		}
		if err := xmlEnc.Encode(contact); err != nil {
			log.Fatal(err)
		}
	}
	if err := xmlEnc.Close(); err != nil {
		log.Fatal(err)
	}
	fmt.Println(strings.Contains(out.String(), "<fn><text>Jane Doe</text></fn>"))
	// Output: true
}

// Convert an xCard document back to vCard 4 text, and bridge to the legacy
// Card type.
func ExampleNewVCard4Encoder_xmlToText() {
	xmlDoc := `<vcards xmlns="urn:ietf:params:xml:ns:vcard-4.0">` +
		`<vcard><fn><text>Jane Doe</text></fn>` +
		`<email><parameters><type><text>work</text></type></parameters>` +
		`<text>jane@example.com</text></email></vcard></vcards>`

	xmlDec := vcard.NewXCardDecoder(strings.NewReader(xmlDoc))
	var text bytes.Buffer
	textEnc := vcard.NewVCard4Encoder(&text)
	for {
		contact, err := xmlDec.Decode()
		if err == io.EOF {
			break
		}
		if err != nil {
			log.Fatal(err)
		}
		if err := textEnc.Encode(contact); err != nil {
			log.Fatal(err)
		}

		// The lossless Contact can also be exposed through the legacy API.
		card, err := contact.Card()
		if err != nil {
			log.Fatal(err)
		}
		fmt.Println(card.PreferredValue(vcard.FieldEmail))
	}
	fmt.Print(text.String())
	// Output:
	// jane@example.com
	// BEGIN:VCARD
	// VERSION:4.0
	// FN:Jane Doe
	// EMAIL;TYPE=work:jane@example.com
	// END:VCARD
}
