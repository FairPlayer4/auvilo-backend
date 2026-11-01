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

// DiensteSperren stellt alle Cloud-Run-Dienste der Region auf "nur intern".
// Anfragen aus dem Internet werden dann abgewiesen, ohne dass eine Instanz
// startet – es entstehen keine Rechenkosten mehr. Rückgängig: Ingress wieder
// auf "all" stellen (z. B. durch erneutes `tofu apply`).
//
// KRITISCH: Fehler bei einzelnen Diensten halten die anderen nicht auf.
func (p *gcpPlattform) DiensteSperren(ctx context.Context) (int, error) {
	eltern := fmt.Sprintf("projects/%s/locations/%s", p.projekt, p.region)
	it := p.run.ListServices(ctx, &runpb.ListServicesRequest{Parent: eltern})

	gesperrt := 0
	var fehler []error
	for {
		dienst, err := it.Next()
		if errors.Is(err, iterator.Done) {
			break
		}
		if err != nil {
			return gesperrt, fmt.Errorf("Dienste auflisten: %w", err)
		}
		if kurzname(dienst.Name) == p.eigenerDienst {
			continue
		}
		if dienst.Ingress == runpb.IngressTraffic_INGRESS_TRAFFIC_INTERNAL_ONLY {
			continue
		}
		dienst.Ingress = runpb.IngressTraffic_INGRESS_TRAFFIC_INTERNAL_ONLY
		op, err := p.run.UpdateService(ctx, &runpb.UpdateServiceRequest{Service: dienst})
		if err == nil {
			_, err = op.Wait(ctx)
		}
		if err != nil {
			// Weitermachen: Ein Dienst mit Fehler soll die anderen nicht aufhalten.
			fehler = append(fehler, fmt.Errorf("%s: %w", dienst.Name, err))
			continue
		}
		gesperrt++
	}
	return gesperrt, errors.Join(fehler...)
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

// kurzname macht aus "projects/p/locations/r/services/name" den Namen "name".
func kurzname(voll string) string {
	for i := len(voll) - 1; i >= 0; i-- {
		if voll[i] == '/' {
			return voll[i+1:]
		}
	}
	return voll
}
