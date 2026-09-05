package vhtlc

import (
	"encoding/hex"
	"fmt"

	arklib "github.com/arkade-os/arkd/pkg/ark-lib"
	"github.com/arkade-os/arkd/pkg/ark-lib/script"
	"github.com/btcsuite/btcd/btcec/v2"
	"github.com/btcsuite/btcd/btcec/v2/schnorr"
	"github.com/btcsuite/btcd/chainhash/v2"
	"github.com/btcsuite/btcd/txscript/v2"
	"golang.org/x/crypto/ripemd160" //nolint:staticcheck
)

// Params carries the parameter tuple a Boltz submarine swap's VHTLC
// script is derived from.
//
// Populated from a CreateSubmarineSwapResponse: preimage hash from the
// BOLT11 invoice (SHA-256 of the payment secret — Boltz applies
// ripemd160 internally so callers pass the SHA-256 digest here); sender
// = our own arkade account key; receiver = Boltz's claimPublicKey;
// server = arkd operator pubkey; the timeout values from
// response.timeoutBlockHeights.
type Params struct {
	// PreimageHash is the 32-byte SHA-256(preimage) — typically
	// the payment_hash of the BOLT11 invoice. Build applies
	// ripemd160 before committing.
	PreimageHash []byte

	Sender   *btcec.PublicKey // our key, x-only usable
	Receiver *btcec.PublicKey // Boltz's claim key
	Server   *btcec.PublicKey // arkd operator signer key

	RefundLocktime                  arklib.AbsoluteLocktime
	UnilateralClaim                 arklib.RelativeLocktime
	UnilateralRefund                arklib.RelativeLocktime
	UnilateralRefundWithoutReceiver arklib.RelativeLocktime

	// NonInteractiveClaim, when non-nil, adds the optional seventh
	// leaf (see NonInteractiveClaimParams). Leave nil for the
	// classic six-leaf VHTLC — the shape every Boltz swap uses
	// today.
	NonInteractiveClaim *NonInteractiveClaimParams
}

// NonInteractiveClaimParams enables the optional non-interactive claim
// leaf on a VHTLC. When present, a solver bot can claim the VHTLC on
// the receiver's behalf by revealing the preimage, without any
// signature from the receiver: the leaf is a 2-of-2 of the ark server
// and an *emulator-tweaked* key, where the tweak commits to an arkade
// introspection script that pins the claim output to the receiver's
// own pkScript (for at least the input's value). So the receiver can
// tolerate the solver spending for it — the covenant forces the money
// to land in their wallet.
//
// Both fields come from whoever negotiated the swap; they are not
// derivable from the other VHTLC parameters, so a client can only
// reconstruct such a VHTLC if its counterparty discloses them (see
// Build's note on Boltz).
type NonInteractiveClaimParams struct {
	// ReceiverPkScript is the 34-byte P2TR pkScript the claim is
	// forced to pay (the receiver's wallet output script).
	ReceiverPkScript []byte

	// EmulatorPubKey is the solver's emulator signing key, which the
	// leaf commits to in tweaked form.
	EmulatorPubKey *btcec.PublicKey
}

// VHTLC tapscript leaf templates, expressed in btcd's ScriptTemplate DSL.
// Integer timelocks (the CLTV locktime and the CSV sequences) are emitted
// through the template's integer path, which routes via
// ScriptBuilder.AddInt64 — the same minimal CScriptNum encoding ark-lib
// and @arkade-os/sdk use. That shared encoding is what keeps the rendered
// leaves byte-identical across all three implementations, which is
// load-bearing: a one-opcode drift changes the taproot output key, breaks
// address matching, and sends funds to a script nobody can spend.
// noformat
const (
	// vhtlcClaimTmpl: receiver + server claim cooperatively by revealing
	// the preimage (the OP_HASH160 ... OP_EQUAL condition).
	vhtlcClaimTmpl = "OP_HASH160 {{ hex .PreimageHash }} OP_EQUAL " +
		"OP_VERIFY {{ hex .Receiver }} OP_CHECKSIGVERIFY " +
		"{{ hex .Server }} OP_CHECKSIG"

	// vhtlcRefundTmpl: sender + receiver + server refund cooperatively,
	// with no timelock.
	vhtlcRefundTmpl = "{{ hex .Sender }} OP_CHECKSIGVERIFY " +
		"{{ hex .Receiver }} OP_CHECKSIGVERIFY " +
		"{{ hex .Server }} OP_CHECKSIG"

	// vhtlcRefundWithoutReceiverTmpl: sender + server refund once the
	// absolute refund locktime (CLTV) elapses.
	vhtlcRefundWithoutReceiverTmpl = "{{ .RefundLocktime }} " +
		"OP_CHECKLOCKTIMEVERIFY OP_DROP {{ hex .Sender }} " +
		"OP_CHECKSIGVERIFY {{ hex .Server }} OP_CHECKSIG"

	// vhtlcUnilateralClaimTmpl: receiver claims alone after a relative
	// (CSV) delay, still gated by the preimage reveal.
	vhtlcUnilateralClaimTmpl = "OP_HASH160 {{ hex .PreimageHash }} " +
		"OP_EQUAL OP_VERIFY {{ .ClaimSeq }} OP_CHECKSEQUENCEVERIFY " +
		"OP_DROP {{ hex .Receiver }} OP_CHECKSIG"

	// vhtlcUnilateralRefundTmpl: sender + receiver refund after a relative
	// (CSV) delay.
	vhtlcUnilateralRefundTmpl = "{{ .RefundSeq }} " +
		"OP_CHECKSEQUENCEVERIFY OP_DROP {{ hex .Sender }} " +
		"OP_CHECKSIGVERIFY {{ hex .Receiver }} OP_CHECKSIG"

	// vhtlcUnilateralRefundWithoutReceiverTmpl: sender refunds alone after
	// a (longer) relative (CSV) delay.
	vhtlcUnilateralRefundWithoutReceiverTmpl = "{{ .RWRSeq }} " +
		"OP_CHECKSEQUENCEVERIFY OP_DROP {{ hex .Sender }} OP_CHECKSIG"

	// p2trPkScriptLen is the length of a P2TR scriptPubKey: OP_1
	// OP_DATA_32 <32-byte witness program>.
	p2trPkScriptLen = 34
)

func f() {}
