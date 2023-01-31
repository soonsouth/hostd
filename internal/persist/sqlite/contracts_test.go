package sqlite

import (
	"errors"
	"fmt"
	"path/filepath"
	"testing"

	"go.sia.tech/hostd/host/contracts"
	"go.sia.tech/siad/crypto"
	"go.sia.tech/siad/types"
	"lukechampine.com/frand"
)

func rootsEqual(a, b []crypto.Hash) error {
	if len(a) != len(b) {
		return errors.New("length mismatch")
	}
	for i := range a {
		if a[i] != b[i] {
			return fmt.Errorf("root %v mismatch: expected %v, got %v", i, a[i], b[i])
		}
	}
	return nil
}

func TestUpdateContractRoots(t *testing.T) {
	db, err := OpenDatabase(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	// add a contract to the database
	contract := types.FileContractRevision{
		ParentID:          frand.Entropy256(),
		NewRevisionNumber: 1,
		NewWindowStart:    100,
		NewWindowEnd:      200,
	}

	if err := db.AddContract(contract, []types.Transaction{}, types.ZeroCurrency, 0, frand.Bytes(64), frand.Bytes(64)); err != nil {
		t.Fatal(err)
	}

	// add some sector roots
	roots := make([]crypto.Hash, 10)
	for i := range roots {
		roots[i] = frand.Entropy256()
	}

	err = db.UpdateContracts(func(tx contracts.UpdateContractTransaction) error {
		for _, root := range roots {
			if err := tx.AppendSector(contract.ParentID, root); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}

	// verify the roots were added in the correct order
	dbRoots, err := db.SectorRoots(contract.ParentID, 0, 100)
	if err != nil {
		t.Fatal(err)
	} else if err = rootsEqual(roots, dbRoots); err != nil {
		t.Fatal(err)
	}

	// swap two roots
	i, j := 5, 8
	roots[i], roots[j] = roots[j], roots[i]
	err = db.UpdateContracts(func(tx contracts.UpdateContractTransaction) error {
		return tx.SwapSectors(contract.ParentID, uint64(i), uint64(j))
	})
	if err != nil {
		t.Fatal(err)
	}

	// verify the roots were swapped
	dbRoots, err = db.SectorRoots(contract.ParentID, 0, 100)
	if err != nil {
		t.Fatal(err)
	} else if err = rootsEqual(roots, dbRoots); err != nil {
		t.Fatal(err)
	}

	// trim the last 3 roots
	toRemove := 3
	roots = roots[:len(roots)-toRemove]
	err = db.UpdateContracts(func(tx contracts.UpdateContractTransaction) error {
		return tx.TrimSectors(contract.ParentID, uint64(toRemove))
	})
	if err != nil {
		t.Fatal(err)
	}

	// verify the roots were removed
	dbRoots, err = db.SectorRoots(contract.ParentID, 0, 100)
	if err != nil {
		t.Fatal(err)
	} else if err = rootsEqual(roots, dbRoots); err != nil {
		t.Fatal(err)
	}

	// swap a root outside of the range, should fail
	err = db.UpdateContracts(func(tx contracts.UpdateContractTransaction) error {
		return tx.SwapSectors(contract.ParentID, 0, 100)
	})
	if err == nil {
		t.Fatal("expected error")
	}

	// verify the roots stayed the same
	dbRoots, err = db.SectorRoots(contract.ParentID, 0, 100)
	if err != nil {
		t.Fatal(err)
	} else if err = rootsEqual(roots, dbRoots); err != nil {
		t.Fatal(err)
	}
}