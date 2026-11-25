package web

import (
	"database/sql"

	"github.com/Dealisto/almanaut/internal/domain"
	"github.com/Dealisto/almanaut/internal/store"
	"github.com/Dealisto/almanaut/internal/webhook"
)

func portConnectionEffects(ports *store.PortRepo, rs resource[domain.Port]) func(*sql.Tx, *domain.Port, *domain.Port) (mutationEffects, error) {
	return func(tx *sql.Tx, old, next *domain.Port) (mutationEffects, error) {
		var id, peerID int64
		if old != nil {
			id = old.ID
		}
		if next != nil {
			peerID = next.PeerPortID
		}
		before, err := ports.WithTx(tx).ConnectionPeers(id, peerID)
		if err != nil {
			return nil, err
		}
		return recordPortEffects(tx, rs, before), nil
	}
}

func nicPortEffects(ports *store.PortRepo, rs resource[domain.Port]) func(*sql.Tx, *domain.NIC, *domain.NIC) (mutationEffects, error) {
	return func(tx *sql.Tx, old, next *domain.NIC) (mutationEffects, error) {
		if old == nil {
			return nil, nil
		}
		before, err := ports.WithTx(tx).ForNIC(old.ID)
		if err != nil {
			return nil, err
		}
		return recordPortEffects(tx, rs, before), nil
	}
}

func recordPortEffects(tx *sql.Tx, rs resource[domain.Port], before []domain.Port) mutationEffects {
	return func(d handlerDeps, actor string, events *[]webhook.Event) error {
		for _, old := range before {
			current, err := rs.repo.GetTx(tx, old.ID)
			if err != nil {
				return err
			}
			if err := rs.recordUpdateTx(tx, d, old, current, actor, events); err != nil {
				return err
			}
		}
		return nil
	}
}
