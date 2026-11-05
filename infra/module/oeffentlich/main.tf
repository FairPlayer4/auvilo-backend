# Macht einen Cloud-Run-Dienst öffentlich aufrufbar ("allUsers" als Invoker),
# aber nicht gegen den Kill-Switch: Der sperrt, indem er diese Bindung
# entfernt (killswitch/sperren.go), und ein `tofu apply` darf sie dann nicht
# einfach neu anlegen (KO3, docs/anforderungen.md).
#
# Jeder öffentliche Cloud-Run-Dienst bekommt seine Bindung über dieses Modul,
# mit depends_on auf den Dienst (siehe server.tf).

terraform {
  required_providers {
    google = {
      source = "hashicorp/google"
    }
  }
}

variable "name" {
  description = "Name des Cloud-Run-Dienstes"
  type        = string
}

variable "location" {
  description = "Region des Dienstes"
  type        = string
}

variable "oeffnen" {
  description = "true legt die Bindung auch an, wenn der Dienst gerade gesperrt ist"
  type        = bool
}

# Wie die Bindungen in GCP wirklich stehen (nicht wie im Zustand). Mit
# depends_on am Modulaufruf liest OpenTofu erst, nachdem der Dienst angelegt
# oder geändert ist.
data "google_cloud_run_v2_service_iam_policy" "dienst" {
  location = var.location
  name     = var.name
}

locals {
  # Offen = "allUsers" hat irgendeine Rolle. Eine leere Richtlinie hat keine
  # "bindings" (try).
  offen = anytrue([
    for b in try(jsondecode(data.google_cloud_run_v2_service_iam_policy.dienst.policy_data).bindings, []) :
    contains(b.members, "allUsers")
  ])
}

resource "google_cloud_run_v2_service_iam_member" "oeffentlich" {
  location = var.location
  name     = var.name
  role     = "roles/run.invoker"
  member   = "allUsers"

  lifecycle {
    # KRITISCH (KO3): Hat der Kill-Switch die Bindung entfernt, würde `tofu
    # apply` sie neu anlegen und den Dienst wieder öffnen. Das geht nur
    # bewusst: `tofu apply -var=gesperrte_dienste_oeffnen=true`.
    # Ein neuer Dienst braucht das einmal ebenso (vorher ist niemand berechtigt).
    precondition {
      condition     = var.oeffnen || local.offen
      error_message = "Der Dienst ${var.name} ist für das Internet gesperrt (Kill-Switch?). Erst die Ursache klären, dann öffnen mit: tofu apply -var=gesperrte_dienste_oeffnen=true"
    }
  }
}
