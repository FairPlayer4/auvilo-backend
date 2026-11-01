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
    # nutzen diese Variable, und sie lässt nur europe-* zu.
    condition     = can(regex("^europe-[a-z]+[0-9]+$", var.region))
    error_message = "Nur EU-Regionen (europe-…) sind erlaubt."
  }
}

variable "rechnungskonto" {
  description = "ID des Rechnungskontos (z. B. 012345-6789AB-CDEF01). Steht in werte.auto.tfvars, nicht im Repo."
  type        = string
}

variable "budget_eur" {
  description = "Monatliches Budget in Euro"
  type        = number
  default     = 20
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
}

variable "github_repo" {
  description = "GitHub-Repo, das per Workload Identity Federation deployen darf"
  type        = string
  default     = "FairPlayer4/avlreminder-backend"
}
