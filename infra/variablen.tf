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
