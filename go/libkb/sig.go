package libkb

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io/ioutil"
	"strings"

	keybase1 "github.com/keybase/client/protocol/go"
	jsonw "github.com/keybase/go-jsonw"
	"golang.org/x/crypto/openpgp"
	"golang.org/x/crypto/openpgp/armor"
)

func ComputeSigIDFromSigBody(body []byte) keybase1.SigID {
	return keybase1.SigIDFromBytes(sha256.Sum256(body))
}

func GetSigID(w *jsonw.Wrapper, suffix bool) (keybase1.SigID, error) {
	s, err := w.GetString()
	if err != nil {
		return "", err
	}
	return keybase1.SigIDFromString(s, suffix)
}

type ParsedSig struct {
	Block       *armor.Block
	SigBody     []byte
	MD          *openpgp.MessageDetails
	LiteralData []byte
}

func PgpOpenSig(armored string) (ps *ParsedSig, err error) {
	pso := ParsedSig{}
	pso.Block, err = armor.Decode(strings.NewReader(armored))
	if err != nil {
		return
	}
	pso.SigBody, err = ioutil.ReadAll(pso.Block.Body)
	if err != nil {
		return
	}
	ps = &pso
	return
}

// OpenSig takes an armored PGP or Keybase signature and opens
// the armor.  It will return the body of the signature, the
// sigID of the body, or an error if it didn't work out.
func OpenSig(armored string) (ret []byte, id keybase1.SigID, err error) {
	if isPgp(armored) {
		var ps *ParsedSig
		if ps, err = PgpOpenSig(armored); err == nil {
			ret = ps.SigBody
			id = ps.ID()
		}
	} else {
		if ret, err = KbOpenSig(armored); err == nil {
			id = ComputeSigIDFromSigBody(ret)
		}
	}
	return
}

func isPgp(armored string) bool {
	return strings.HasPrefix(armored, "-----BEGIN PGP")
}

func SigAssertPayload(armored string, expected []byte) (sigID keybase1.SigID, err error) {
	if isPgp(armored) {
		return SigAssertPgpPayload(armored, expected)
	}
	return SigAssertKbPayload(armored, expected)
}

func SigAssertPgpPayload(armored string, expected []byte) (sigID keybase1.SigID, err error) {
	var ps *ParsedSig
	ps, err = PgpOpenSig(armored)
	if err != nil {
		return
	}
	if err = ps.AssertPayload(expected); err != nil {
		ps = nil
		return
	}
	sigID = ps.ID()
	return
}

func (ps *ParsedSig) AssertPayload(expected []byte) error {

	ring := EmptyKeyRing{}
	md, err := openpgp.ReadMessage(bytes.NewReader(ps.SigBody), ring, nil, nil)
	if err != nil {
		return err
	}
	data, err := ioutil.ReadAll(md.UnverifiedBody)
	if err != nil {
		return err
	}
	if !FastByteArrayEq(data, expected) {
		err = fmt.Errorf("Signature did not contain expected text")
		return err
	}
	return nil
}

func (ps *ParsedSig) Verify(k PgpKeyBundle) (err error) {
	ps.MD, err = openpgp.ReadMessage(bytes.NewReader(ps.SigBody), k, nil, nil)
	if err != nil {
		return
	}
	if !ps.MD.IsSigned || ps.MD.SignedBy == nil {
		err = fmt.Errorf("Message wasn't signed")
		return
	}
	if !k.MatchesKey(ps.MD.SignedBy) {
		err = fmt.Errorf("Got wrong SignedBy key %v",
			hex.EncodeToString(ps.MD.SignedBy.PublicKey.Fingerprint[:]))
		return
	}
	if ps.MD.UnverifiedBody == nil {
		err = fmt.Errorf("no signed material found")
		return
	}

	ps.LiteralData, err = ioutil.ReadAll(ps.MD.UnverifiedBody)
	if err != nil {
		return
	}

	// We'll see a sig error here after reading in the UnverifiedBody above,
	// if there was one to see.
	if err = ps.MD.SignatureError; err != nil {
		return
	}

	if ps.MD.Signature == nil {
		err = fmt.Errorf("No available signature after checking signature")
		return
	}

	// Hopefully by here we've covered all of our bases.
	return nil
}

func (ps *ParsedSig) ID() keybase1.SigID {
	return ComputeSigIDFromSigBody(ps.SigBody)
}
