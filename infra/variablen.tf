variable "projekt" {
  description = "GCP-Projekt-ID"
  type        = string
}

variable "region" {
  description = "Region für alle Ressourcen. Nur EU-Regionen sind erlaubt (Anforderung D1)."
  type        = string
  default     = "europe-west1"

  validation {
    # Ersatz für die Standortrichtlinie einer Organisation: Alle Ressourcen
    # nutzen diese Variable. Positivliste statt "europe-*", denn europe-west2
    # (London) und europe-west6 (Zürich) liegen nicht in der EU.
    condition = contains([
      "europe-central2",   # Warschau
      "europe-north1",     # Finnland
      "europe-north2",     # Stockholm
      "europe-southwest1", # Madrid
      "europe-west1",      # Belgien
      "europe-west3",      # Frankfurt
      "europe-west4",      # Niederlande
      "europe-west8",      # Mailand
      "europe-west9",      # Paris
      "europe-west10",     # Berlin
      "europe-west12",     # Turin
    ], var.region)
    error_message = "Nur Regionen innerhalb der EU sind erlaubt."
  }
}

variable "rechnungskonto" {
  description = "ID des Rechnungskontos (z. B. 012345-6789AB-CDEF01). Steht in werte.auto.tfvars, nicht im Repo."
  type        = string
}

variable "alarm_email" {
  description = "E-Mail-Adresse für Alarme des Kill-Switch. Steht in werte.auto.tfvars, nicht im Repo."
  type        = string
}

variable "budget_eur" {
  description = "Monatliches Budget in Euro"
  type        = number
  default     = 20

  validation {
    # Die Budget-API erwartet ganze Euro.
    condition     = var.budget_eur > 0 && floor(var.budget_eur) == var.budget_eur
    error_message = "Das Budget muss eine ganze Zahl größer 0 sein."
  }
}

variable "schwelle_sperren" {
  description = "Anteil des Budgets, ab dem Cloud Run gesperrt wird (1.0 = 100 %)"
  type        = number
  default     = 1.0
}

variable "gesperrte_dienste_oeffnen" {
  description = "true öffnet vom Kill-Switch gesperrte Dienste wieder (nur für diesen einen Aufruf von tofu apply setzen)"
  type        = bool
  default     = false
}

variable "schwelle_trennen" {
  description = "Anteil des Budgets, ab dem das Projekt vom Rechnungskonto getrennt wird"
  type        = number
  default     = 1.5

  validation {
    # Gleiche Regel wie Schwellen.Pruefe im Kill-Switch – so fällt ein Fehler
    # schon beim Plan auf statt erst, wenn die Funktion jede Meldung ablehnt.
    condition     = var.schwelle_sperren > 0 && var.schwelle_sperren <= var.schwelle_trennen
    error_message = "Es muss gelten: 0 < schwelle_sperren <= schwelle_trennen."
  }
}

variable "github_repo" {
  description = "GitHub-Repo, das per Workload Identity Federation deployen darf"
  type        = string
  default     = "FairPlayer4/avlreminder-backend"
}

# Die Zahl bleibt gleich, auch wenn das Repo umbenannt oder gelöscht und unter
# gleichem Namen neu angelegt wird – der Name allein könnte dann einem fremden
# Repo gehören. Ermitteln: gh api repos/<besitzer>/<repo> --jq .id
variable "github_repo_id" {
  description = "Numerische ID des GitHub-Repos (Prüfung bei Workload Identity Federation)"
  type        = string
  default     = "1441701583"
}

# Client-IDs sind nicht geheim (sie stehen in jeder App). Der Desktop-Client
# gehört zum Kotlin-Test-Client, der Web-Client zur App: Bei „Mit Google
# anmelden“ auf Android ist `aud` des ID-Tokens der Web-Client (serverClientId),
# nicht der Android-Client; deshalb steht der Android-Client hier nicht. Der
# Desktop-Client fliegt zu Beginn der Sicherheitsphase raus (Checkliste in
# docs/b2b-entwurf.md, Punkt 4; Review Lauf 2).
variable "google_client_ids" {
  description = "OAuth-Client-IDs, deren Google-ID-Tokens der Server annimmt"
  type        = list(string)
  default = [
    "82416816442-vka4ve2no02681umqkn8b0rk37ri37qd.apps.googleusercontent.com", # Desktop (Test-Client)
    "82416816442-suvsga1qcl9aaj1u9rtdhbt91ekadh92.apps.googleusercontent.com", # Web (App, serverClientId)
  ]

  validation {
    condition     = length(var.google_client_ids) > 0 && alltrue([for id in var.google_client_ids : endswith(id, ".apps.googleusercontent.com")])
    error_message = "Mindestens eine Client-ID der Form <nummer>-<kennung>.apps.googleusercontent.com."
  }
}

# Nur beim ersten Anlegen des Dienstes benutzt; danach setzt der Deploy das
# Image, und OpenTofu ignoriert es. Das Beispiel-Image von Google beantwortet
# Anfragen mit einer Begrüßungsseite und verarbeitet keine Daten.
variable "server_image_start" {
  description = "Image für das erste Anlegen des Cloud-Run-Dienstes"
  type        = string
  default     = "us-docker.pkg.dev/cloudrun/container/hello"
}

variable "freigegebene_kennungen" {
  description = "Kennungen der Google-Konten, die sich anmelden dürfen (ID7, docs/anforderungen.md). Steht in werte.auto.tfvars; leer = niemand, [\"*\"] = alle."
  type        = list(string)
  default     = []

  validation {
    # Kennungen sind HMAC-SHA256 hexadezimal (anmeldung.Kennung). Der Server
    # startet bei einem Tippfehler nicht; hier fällt er schon im Plan auf.
    # join statt == ["*"]: Eine Liste ist in HCL nie gleich einem Tupel.
    condition     = join(",", var.freigegebene_kennungen) == "*" || alltrue([for k in var.freigegebene_kennungen : can(regex("^[0-9a-f]{64}$", k))])
    error_message = "freigegebene_kennungen: jede Kennung 64 Zeichen 0-9a-f, oder genau [\"*\"] für alle."
  }
}
