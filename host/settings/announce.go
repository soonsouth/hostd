package settings

import (
	"errors"
	"fmt"
	"net"
	"strconv"

	"go.sia.tech/core/types"
	"go.sia.tech/coreutils/chain"
	rhp4 "go.sia.tech/coreutils/rhp/v4"
	"go.uber.org/zap"
)

type (
	// An Announcement contains the host's announced netaddress
	Announcement struct {
		Index   types.ChainIndex `json:"index"`
		Address string           `json:"address"`
	}
)

// Announce announces the host to the network
func (m *ConfigManager) Announce() error {
	// get the current settings
	settings := m.Settings()

	if m.validateNetAddress {
		if err := validateNetAddress(settings.NetAddress); err != nil {
			return fmt.Errorf("failed to validate net address %q: %w", settings.NetAddress, err)
		}
	}

	minerFee := m.chain.RecommendedFee().Mul64(announcementTxnSize)

	cs := m.chain.TipState()
	if cs.Index.Height < cs.Network.HardforkV2.AllowHeight {
		// create a transaction with an announcement
		txn := types.Transaction{
			ArbitraryData: [][]byte{
				chain.HostAnnouncement{
					PublicKey:  m.hostKey.PublicKey(),
					NetAddress: settings.NetAddress,
				}.ToArbitraryData(m.hostKey),
			},
			MinerFees: []types.Currency{minerFee},
		}

		// fund the transaction
		toSign, err := m.wallet.FundTransaction(&txn, minerFee, true)
		if err != nil {
			return fmt.Errorf("failed to fund transaction: %w", err)
		}
		m.wallet.SignTransaction(&txn, toSign, types.CoveredFields{WholeTransaction: true})
		txnset := append(m.chain.UnconfirmedParents(txn), txn)
		if _, err := m.chain.AddPoolTransactions(txnset); err != nil {
			m.wallet.ReleaseInputs([]types.Transaction{txn}, nil)
			return fmt.Errorf("failed to add transaction to pool: %w", err)
		}
		m.syncer.BroadcastTransactionSet(txnset)
		m.log.Debug("broadcast announcement", zap.String("transactionID", txn.ID().String()), zap.String("netaddress", settings.NetAddress), zap.String("cost", minerFee.ExactString()))
	} else {
		// create a v2 transaction with an announcement
		txn := types.V2Transaction{
			Attestations: []types.Attestation{
				chain.V2HostAnnouncement{
					{Protocol: rhp4.ProtocolTCPSiaMux, Address: settings.NetAddress}, // TODO: this isn't correct
				}.ToAttestation(cs, m.hostKey),
			},
			MinerFee: minerFee,
		}
		basis, toSign, err := m.wallet.FundV2Transaction(&txn, minerFee, true)
		if err != nil {
			return fmt.Errorf("failed to fund transaction: %w", err)
		}
		m.wallet.SignV2Inputs(&txn, toSign)
		basis, txnset, err := m.chain.V2TransactionSet(basis, txn)
		if err != nil {
			m.wallet.ReleaseInputs(nil, []types.V2Transaction{txn})
			return fmt.Errorf("failed to create transaction set: %w", err)
		} else if _, err := m.chain.AddV2PoolTransactions(basis, txnset); err != nil {
			m.wallet.ReleaseInputs(nil, []types.V2Transaction{txn})
			return fmt.Errorf("failed to add transaction to pool: %w", err)
		}
		m.syncer.BroadcastV2TransactionSet(cs.Index, txnset)
		m.log.Debug("broadcast v2 announcement", zap.String("transactionID", txn.ID().String()), zap.String("netaddress", settings.NetAddress), zap.String("cost", minerFee.ExactString()))
	}
	return nil
}

func validateNetAddress(netaddress string) error {
	host, port, err := net.SplitHostPort(netaddress)
	if err != nil {
		return fmt.Errorf("failed to split net address: %w", err)
	}

	// Check that the host is not empty or localhost.
	if host == "" {
		return errors.New("empty net address")
	} else if host == "localhost" {
		return errors.New("net address cannot be localhost")
	}

	// Check that the port is a valid number.
	n, err := strconv.Atoi(port)
	if err != nil {
		return fmt.Errorf("failed to parse port: %w", err)
	} else if n < 1 || n > 65535 {
		return errors.New("port must be between 1 and 65535")
	}

	// If the host is an IP address, check that it is a public IP address.
	ip := net.ParseIP(host)
	if ip != nil {
		if ip.IsLoopback() || ip.IsPrivate() || !ip.IsGlobalUnicast() {
			return errors.New("only public IP addresses allowed")
		}
		return nil
	}
	return nil
}
