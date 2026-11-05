package killswitch

import (
	"context"
	"errors"
	"fmt"

	billing "cloud.google.com/go/billing/apiv1"
	"cloud.google.com/go/billing/apiv1/billingpb"
	"cloud.google.com/go/iam/apiv1/iampb"
	run "cloud.google.com/go/run/apiv2"
	"cloud.google.com/go/run/apiv2/runpb"
	"google.golang.org/api/iterator"
)

// gcpPlattform ist die echte Umsetzung von Plattform mit den Google-Clients.
// Die Anmeldung läuft automatisch über das Dienstkonto der Funktion.
type gcpPlattform struct {
	projekt       string
	region        string
	eigenerDienst string // die Funktion selbst läuft als Cloud-Run-Dienst und wird nicht gesperrt
	run           *run.ServicesClient
	billing       *billing.CloudBillingClient
}

func neueGCPPlattform(ctx context.Context, projekt, region, eigenerDienst string) (*gcpPlattform, error) {
	if projekt == "" || region == "" {
		return nil, errors.New("Umgebungsvariablen PROJEKT und REGION müssen gesetzt sein")
	}
	rc, err := run.NewServicesClient(ctx)
	if err != nil {
		return nil, fmt.Errorf("Cloud-Run-Client: %w", err)
	}
	bc, err := billing.NewCloudBillingClient(ctx)
	if err != nil {
		// Der erste Fehler ist der interessante; ein Fehler beim Schließen kommt dazu.
		return nil, errors.Join(fmt.Errorf("Billing-Client: %w", err), rc.Close())
	}
	return &gcpPlattform{projekt: projekt, region: region, eigenerDienst: eigenerDienst, run: rc, billing: bc}, nil
}

// Schliessen gibt die Verbindungen frei. errors.Join fasst beide möglichen
// Fehler zusammen (nil, wenn beide nil sind).
func (p *gcpPlattform) Schliessen() error {
	return errors.Join(p.run.Close(), p.billing.Close())
}

// DiensteSperren erfüllt Plattform. Die eigentliche Logik steht in
// SperreAlle (sperren.go); hier sind nur die Cloud-Run-Aufrufe.
// Rückgängig: `tofu apply -var=gesperrte_dienste_oeffnen=true` (docs/stand.md).
func (p *gcpPlattform) DiensteSperren(ctx context.Context) (Sperrung, error) {
	return SperreAlle(ctx, p, p.eigenerDienst)
}

// Dienste listet alle Cloud-Run-Dienste der Region (Teil von CloudRun).
func (p *gcpPlattform) Dienste(ctx context.Context) ([]Dienst, error) {
	eltern := fmt.Sprintf("projects/%s/locations/%s", p.projekt, p.region)
	it := p.run.ListServices(ctx, &runpb.ListServicesRequest{Parent: eltern})
	var dienste []Dienst
	for {
		d, err := it.Next()
		if errors.Is(err, iterator.Done) {
			return dienste, nil
		}
		if err != nil {
			return nil, err
		}
		// KRITISCH: Ist die Richtlinie eines Dienstes gerade nicht lesbar, gilt
		// er als offen. Sperre liest sie noch einmal und meldet den Fehler, falls
		// es wieder scheitert; die anderen Dienste werden trotzdem gesperrt.
		pol, err := p.richtlinie(ctx, d.Name)
		dienste = append(dienste, Dienst{Name: d.Name, Gesperrt: err == nil && !istOeffentlich(pol)})
	}
}

// richtlinie liest die IAM-Richtlinie eines Dienstes. Version 3 liefert auch
// Rollen mit Bedingungen; ohne sie gingen diese beim Zurückschreiben verloren.
func (p *gcpPlattform) richtlinie(ctx context.Context, name string) (*iampb.Policy, error) {
	return p.run.GetIamPolicy(ctx, &iampb.GetIamPolicyRequest{
		Resource: name,
		Options:  &iampb.GetPolicyOptions{RequestedPolicyVersion: 3},
	})
}

// Sperre entzieht dem Internet das Aufrufrecht (Teil von CloudRun, Logik in
// ohneOeffentlich). Die Richtlinie wird frisch gelesen und mit ihrem Etag
// zurückgeschrieben: Hat sie inzwischen jemand anders geändert, lehnt die API
// ab, statt dessen Änderung zu überschreiben (Fehler im Log, nächste
// Budget-Meldung versucht es wieder). Braucht kein actAs (siehe ohneOeffentlich).
func (p *gcpPlattform) Sperre(ctx context.Context, name string) (bool, error) {
	pol, err := p.richtlinie(ctx, name)
	if err != nil {
		return false, fmt.Errorf("Richtlinie lesen: %w", err)
	}
	if !ohneOeffentlich(pol) {
		return false, nil // inzwischen schon gesperrt
	}
	pol.Version = 3
	if _, err := p.run.SetIamPolicy(ctx, &iampb.SetIamPolicyRequest{Resource: name, Policy: pol}); err != nil {
		return false, err
	}
	return true, nil
}

// AbrechnungTrennen trennt das Projekt vom Rechnungskonto (leerer Kontoname).
// Wieder verbinden geht nur von Hand (`gcloud billing projects link`).
//
// KRITISCH: Danach stoppen alle kostenpflichtigen Dienste des Projekts.
func (p *gcpPlattform) AbrechnungTrennen(ctx context.Context) error {
	_, err := p.billing.UpdateProjectBillingInfo(ctx, &billingpb.UpdateProjectBillingInfoRequest{
		Name:               "projects/" + p.projekt,
		ProjectBillingInfo: &billingpb.ProjectBillingInfo{BillingAccountName: ""},
	})
	return err
}
