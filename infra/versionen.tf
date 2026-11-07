# OpenTofu-Grundeinstellungen: benötigte Provider und Ablage des Zustands.
#
# Der Zustand (welche Ressourcen OpenTofu verwaltet) liegt in einem Bucket in
# europe-west1. Den Bucket hat der Bootstrap per gcloud angelegt (Befehle in
# docs/gcp-einrichtung.md), weil OpenTofu seinen eigenen Zustands-Speicher
# nicht selbst anlegen kann.

terraform {
  # 1.11: ephemeral-Ressourcen und write-only-Attribute (Pfeffer, server.tf).
  required_version = ">= 1.11"

  required_providers {
    google = {
      source = "hashicorp/google"
      # Nur Minor-Updates automatisch; ein neues Major (9.x) kann Ressourcen
      # umbenennen oder Standardwerte ändern und wird bewusst angehoben.
      version = "~> 8.0"
    }
    archive = {
      source  = "hashicorp/archive"
      version = ">= 2.4"
    }
  }

  backend "gcs" {
    bucket = "avlreminder-exp-1ef2-tfstate"
    prefix = "infra"
  }
}

provider "google" {
  project = var.projekt
  region  = var.region

  # Budgets gehören zum Rechnungskonto, nicht zum Projekt. Mit diesen zwei
  # Einstellungen werden solche API-Aufrufe trotzdem über unser Projekt
  # abgerechnet und kontingentiert (sonst schlägt das Anlegen fehl).
  billing_project       = var.projekt
  user_project_override = true
}
