package app

import (
	"bytes"

	"github.com/celestiaorg/celestia-app/x/payment/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	abci "github.com/tendermint/tendermint/abci/types"
	"github.com/tendermint/tendermint/pkg/da"
)

const (
	rejectedPropBlockLog = "Rejected proposal block:"
)

func (app *App) ProcessProposal(req abci.RequestProcessProposal) abci.ResponseProcessProposal {
	// Check for message inclusion:
	//  - each MsgPayForMessage included in a block should have a corresponding message also in the block data
	//  - the commitment in each PFM should match that of its corresponding message
	//  - there should be no unpaid for messages

	// extract the commitments from any MsgPayForMessages in the block
	commitments := make(map[string]struct{})
	for _, rawTx := range req.BlockData.Txs {
		tx, err := MalleatedTxDecoder(app.txConfig.TxDecoder())(rawTx)
		if err != nil {
			continue
		}

		for _, msg := range tx.GetMsgs() {
			if sdk.MsgTypeURL(msg) != types.URLMsgPayforMessage {
				continue
			}

			pfm, ok := msg.(*types.MsgPayForMessage)
			if !ok {
				continue
			}

			commitments[string(pfm.MessageShareCommitment)] = struct{}{}
		}

	}

	// quickly compare the number of PFMs and messages, if they aren't
	// identical, then  we already know this block is invalid
	if len(commitments) != len(req.BlockData.Messages.MessagesList) {
		return abci.ResponseProcessProposal{
			Result: abci.ResponseProcessProposal_REJECT,
		}
	}

	// iterate through all of the messages and ensure that a PFM with the exact
	// commitment exists
	for _, msg := range req.BlockData.Messages.MessagesList {
		commit, err := types.CreateCommitment(req.BlockData.OriginalSquareSize, msg.NamespaceId, msg.Data)
		if err != nil {
			app.Logger().Error(
				rejectedPropBlockLog,
				"reason",
				"failure to create commitment for included message",
				"error",
				err.Error(),
			)
			return abci.ResponseProcessProposal{
				Result: abci.ResponseProcessProposal_REJECT,
			}
		}

		if _, has := commitments[string(commit)]; !has {
			app.Logger().Info(rejectedPropBlockLog, "reason", "missing MsgPayForMessage for included message")
			return abci.ResponseProcessProposal{
				Result: abci.ResponseProcessProposal_REJECT,
			}
		}

	}

	// check that the data matches the data hash
	dataSquare, _, err := WriteSquare(app.txConfig, req.BlockData.OriginalSquareSize, req.BlockData)
	if err != nil {
		// todo(evan): see if we can get rid of this panic
		panic(err)
	}

	eds, err := da.ExtendShares(req.BlockData.OriginalSquareSize, dataSquare)
	if err != nil {
		app.Logger().Error(
			rejectedPropBlockLog,
			"reason",
			"failure to erasure the data square",
			"error",
			err.Error(),
		)
		return abci.ResponseProcessProposal{
			Result: abci.ResponseProcessProposal_REJECT,
		}
	}

	dah := da.NewDataAvailabilityHeader(eds)

	if !bytes.Equal(dah.Hash(), req.Header.DataHash) {
		app.Logger().Info(
			rejectedPropBlockLog,
			"reason",
			"proposed data root differs from calculated data root",
		)
		return abci.ResponseProcessProposal{
			Result: abci.ResponseProcessProposal_REJECT,
		}
	}

	return abci.ResponseProcessProposal{
		Result: abci.ResponseProcessProposal_ACCEPT,
	}
}
