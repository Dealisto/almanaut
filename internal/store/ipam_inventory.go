package store

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/Dealisto/almanaut/internal/domain"
)

// IPAMHosts combines legacy host addresses and explicit assignments without
// modifying either inventory. Non-host allocations use a separate display ref.
func IPAMHosts(db *sql.DB) ([]domain.Host, error) {
	tx, err := db.BeginTx(context.Background(), &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	hosts, err := NewHostRepo(db).WithTx(tx).List()
	if err != nil {
		return nil, err
	}
	return ipamHostsTx(tx, hosts)
}
func ipamHostsTx(tx *sql.Tx, hosts []domain.Host) ([]domain.Host, error) {
	interfaces, err := (&InterfaceRepo{db: tx}).List()
	if err != nil {
		return nil, err
	}
	addresses, err := (&IPAddressRepo{db: tx}).List()
	if err != nil {
		return nil, err
	}
	out := make([]domain.Host, len(hosts))
	copy(out, hosts)
	byHost := map[int64]int{}
	for i, h := range out {
		byHost[h.ID] = i
		out[i].IPs = append([]string(nil), h.IPs...)
	}
	hardware, err := (&HardwareRepo{db: tx}).List()
	if err != nil {
		return nil, err
	}
	hardwareNames := map[int64]string{}
	for _, v := range hardware {
		hardwareNames[v.ID] = v.Name
	}
	byHardware := map[int64]int{}
	byInterface := map[int64]domain.NetworkInterface{}
	for _, v := range interfaces {
		byInterface[v.ID] = v
	}
	for _, v := range addresses {
		prefix, err := domain.ParseInterfaceAddress(v.Address)
		if err != nil {
			continue
		}
		ip := prefix.Addr().String()
		nic, ok := byInterface[v.InterfaceID]
		if ok && nic.OwnerType == "host" {
			if i, exists := byHost[nic.OwnerID]; exists {
				out[i].IPs = append(out[i].IPs, ip)
				continue
			}
		}
		if ok && nic.OwnerType == "hardware" {
			if name, exists := hardwareNames[nic.OwnerID]; exists {
				if i, found := byHardware[nic.OwnerID]; found {
					out[i].IPs = append(out[i].IPs, ip)
				} else {
					byHardware[nic.OwnerID] = len(out)
					out = append(out, domain.Host{ID: -int64(len(out) + 1), Name: name, IPs: []string{ip}, IPAMEntity: &domain.EntityRef{Type: "hardware", ID: nic.OwnerID}})
				}
				continue
			}
		}
		name := v.Address + " (unassigned)"
		if ok {
			name = fmt.Sprintf("%s:%d / %s", nic.OwnerType, nic.OwnerID, nic.Name)
		} else if v.InterfaceID != 0 {
			name = v.Address + " (missing interface)"
		}
		out = append(out, domain.Host{ID: -int64(len(out) + 1), Name: name, IPs: []string{ip}, IPAMEntity: &domain.EntityRef{Type: "ipaddress", ID: v.ID}})
	}
	return out, nil
}
