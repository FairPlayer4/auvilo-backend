package killswitch

import (
	"context"
	"errors"
	"fmt"

	billing "cloud.google.com/go/billing/apiv1"
	"cloud.google.com/go/billing/apiv1/billingpb"
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
// Rückgängig: Ingress wieder auf "all" stellen (z. B. per gcloud).
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
		dienste = append(dienste, Dienst{
			Name:   d.Name,
			Intern: d.Ingress == runpb.IngressTraffic_INGRESS_TRAFFIC_INTERNAL_ONLY,
		})
	}
}

// Sperre stellt einen Dienst auf "nur intern" (Teil von CloudRun). Der Dienst
// wird frisch gelesen, weil die Update-API das vollständige Objekt erwartet,
// und es wird gewartet, bis die Änderung aktiv ist.
func (p *gcpPlattform) Sperre(ctx context.Context, name string) error {
	d, err := p.run.GetService(ctx, &runpb.GetServiceRequest{Name: name})
	if err != nil {
		return err
	}
	d.Ingress = runpb.IngressTraffic_INGRESS_TRAFFIC_INTERNAL_ONLY
	op, err := p.run.UpdateService(ctx, &runpb.UpdateServiceRequest{Service: d})
	if err != nil {
		return err
	}
	_, err = op.Wait(ctx)
	return err
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
