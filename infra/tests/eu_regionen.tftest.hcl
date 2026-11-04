# Anforderung D1: Alles bleibt in der EU.
#
# Ohne GCP-Organisation gibt es keine Standortrichtlinie. Dieser Test ersetzt
# sie: Er plant die Konfiguration mit simulierten Providern (kein GCP-Zugang
# nötig, läuft in der CI) und prüft die Standorte aller Ressourcen.
# Ausführen: im Verzeichnis infra `tofu init -backend=false` und `tofu test`.

# Simulierte Ressourcen bekommen sonst zufällige IDs; die folgenden Werte
# haben das Format, das der Provider bei Verweisen prüft.
mock_provider "google" {
  mock_data "google_project" {
    defaults = {
      number = "123456789"
    }
  }
  mock_resource "google_service_account" {
    defaults = {
      email  = "testkonto@test-projekt.iam.gserviceaccount.com"
      member = "serviceAccount:testkonto@test-projekt.iam.gserviceaccount.com"
      name   = "projects/test-projekt/serviceAccounts/testkonto@test-projekt.iam.gserviceaccount.com"
      id     = "projects/test-projekt/serviceAccounts/testkonto@test-projekt.iam.gserviceaccount.com"
    }
  }
  mock_resource "google_project_iam_custom_role" {
    defaults = {
      name = "projects/test-projekt/roles/killswitchCloudRun"
      id   = "projects/test-projekt/roles/killswitchCloudRun"
    }
  }
  mock_resource "google_iam_workload_identity_pool" {
    defaults = {
      name = "projects/123456789/locations/global/workloadIdentityPools/github"
    }
  }
  mock_resource "google_pubsub_topic" {
    defaults = {
      id = "projects/test-projekt/topics/budget-meldungen"
    }
  }
  mock_resource "google_artifact_registry_repository" {
    defaults = {
      id   = "projects/test-projekt/locations/europe-west1/repositories/container"
      name = "container"
    }
  }
}

# Eigene Werte für die Konten und Repos, um die es in den Rechte-Tests geht. Mit
# den gemeinsamen Vorgaben oben hätten alle Konten dieselbe Adresse, und ein Test
# „gehört nur dem Server-Konto“ bliebe auch mit dem falschen Konto grün.
override_resource {
  target = google_service_account.server
  values = {
    email  = "server@test-projekt.iam.gserviceaccount.com"
    member = "serviceAccount:server@test-projekt.iam.gserviceaccount.com"
    name   = "projects/test-projekt/serviceAccounts/server@test-projekt.iam.gserviceaccount.com"
    id     = "projects/test-projekt/serviceAccounts/server@test-projekt.iam.gserviceaccount.com"
  }
}
override_resource {
  target = google_service_account.bauen
  values = {
    email  = "funktionen-bauen@test-projekt.iam.gserviceaccount.com"
    member = "serviceAccount:funktionen-bauen@test-projekt.iam.gserviceaccount.com"
    name   = "projects/test-projekt/serviceAccounts/funktionen-bauen@test-projekt.iam.gserviceaccount.com"
    id     = "projects/test-projekt/serviceAccounts/funktionen-bauen@test-projekt.iam.gserviceaccount.com"
  }
}
override_resource {
  target = google_artifact_registry_repository.funktionen
  values = {
    id   = "projects/test-projekt/locations/europe-west1/repositories/funktionen"
    name = "funktionen"
  }
}

mock_provider "random" {}

mock_provider "archive" {
  mock_data "archive_file" {
    defaults = {
      output_md5 = "00000000000000000000000000000000"
    }
  }
}

variables {
  projekt        = "test-projekt"
  rechnungskonto = "000000-000000-000000"
  alarm_email    = "test@example.com"
}

run "alle_standorte_liegen_in_der_eu" {
  command = plan

  assert {
    condition     = google_firestore_database.standard.location_id == var.region
    error_message = "Firestore liegt nicht in var.region."
  }
  assert {
    condition     = google_artifact_registry_repository.container.location == var.region
    error_message = "Artifact Registry liegt nicht in var.region."
  }
  assert {
    condition     = google_logging_project_bucket_config.eu.location == var.region
    error_message = "Der Log-Bucket liegt nicht in var.region."
  }
  assert {
    condition     = upper(google_storage_bucket.quellen.location) == upper(var.region)
    error_message = "Der Quellen-Bucket liegt nicht in var.region."
  }
  assert {
    condition     = google_cloudfunctions2_function.killswitch.location == var.region
    error_message = "Die Kill-Switch-Funktion liegt nicht in var.region."
  }
  assert {
    condition     = google_cloudfunctions2_function.killswitch.event_trigger[0].trigger_region == var.region
    error_message = "Der Trigger der Funktion liegt nicht in var.region."
  }
  assert {
    condition     = google_artifact_registry_repository.funktionen.location == var.region
    error_message = "Das Registry-Repo der Funktionen liegt nicht in var.region."
  }
  assert {
    condition     = google_cloud_run_v2_service.server.location == var.region
    error_message = "Der Server liegt nicht in var.region."
  }
  assert {
    condition     = [for r in google_secret_manager_secret.pfeffer.replication[0].user_managed[0].replicas : r.location] == [var.region]
    error_message = "Der Pfeffer wird außerhalb von var.region gespeichert."
  }
  assert {
    condition     = google_pubsub_topic.budget.message_storage_policy[0].allowed_persistence_regions == toset([var.region])
    error_message = "Das Budget-Thema darf Nachrichten außerhalb von var.region speichern."
  }
  assert {
    condition     = upper(google_storage_bucket.datensicherung.location) == upper(var.region)
    error_message = "Der Bucket der Datensicherungen liegt nicht in var.region."
  }
}

# Eine andere EU-Region geht durch, und alles zieht mit (nicht nur die Vorgabe).
run "nimmt_andere_eu_region_an" {
  command = plan

  variables {
    region = "europe-west10" # Berlin
  }

  assert {
    condition     = google_cloud_run_v2_service.server.location == "europe-west10"
    error_message = "Der Server folgt var.region nicht."
  }
  assert {
    condition     = upper(google_storage_bucket.datensicherung.location) == "EUROPE-WEST10"
    error_message = "Der Bucket der Datensicherungen folgt var.region nicht."
  }
}

# Datensicherungen (E-B2b-4): nie öffentlich, ältere Fassungen 7 Tage und
# höchstens 200, Zugriff nur für das Server-Konto.
run "datensicherung_ist_geschuetzt_und_begrenzt" {
  command = plan

  assert {
    condition     = google_storage_bucket.datensicherung.public_access_prevention == "enforced" && google_storage_bucket.datensicherung.uniform_bucket_level_access
    error_message = "Der Bucket der Datensicherungen könnte öffentlich werden."
  }
  assert {
    condition     = google_storage_bucket.datensicherung.versioning[0].enabled
    error_message = "Ohne Versionierung gibt es keine älteren Fassungen."
  }
  assert {
    # Genau zwei Regeln, jede mit nur einer Grenze: Eine zusätzliche Regel (etwa
    # nach Alter) könnte die aktuelle Sicherung löschen, und zwei Grenzen in einer
    # Regel griffen erst, wenn beide zugleich erfüllt sind.
    condition     = length(google_storage_bucket.datensicherung.lifecycle_rule) == 2 && alltrue([for r in google_storage_bucket.datensicherung.lifecycle_rule : one(r.action).type == "Delete" && coalesce(one(r.condition).age, 0) == 0])
    error_message = "Der Bucket hat andere Lebenszyklusregeln als die zwei für ältere Fassungen."
  }
  assert {
    condition     = anytrue([for r in google_storage_bucket.datensicherung.lifecycle_rule : one(r.condition).days_since_noncurrent_time == 7 && coalesce(one(r.condition).num_newer_versions, 0) == 0])
    error_message = "Ältere Fassungen werden nicht nach 7 Tagen gelöscht."
  }
  assert {
    # 201 neuere = die aktuelle und 200 ältere Fassungen bleiben.
    condition     = anytrue([for r in google_storage_bucket.datensicherung.lifecycle_rule : one(r.condition).num_newer_versions == 201 && coalesce(one(r.condition).days_since_noncurrent_time, 0) == 0])
    error_message = "Es bleiben nicht höchstens 200 ältere Fassungen."
  }
  assert {
    condition     = google_storage_bucket.datensicherung.soft_delete_policy[0].retention_duration_seconds == 0
    error_message = "Gelöschte Sicherungen bleiben im Soft Delete liegen (DSGVO Art. 17)."
  }
  assert {
    condition     = google_storage_bucket_iam_member.server_datensicherung.member == "serviceAccount:server@test-projekt.iam.gserviceaccount.com" && google_storage_bucket_iam_member.server_datensicherung.role == "roles/storage.objectUser"
    error_message = "Der Bucket der Datensicherungen gehört nicht (nur) dem Server-Konto."
  }
  assert {
    condition     = contains([for e in google_cloud_run_v2_service.server.template[0].containers[0].env : e.value if e.name == "SICHERUNG_BUCKET"], "test-projekt-datensicherung")
    error_message = "Der Server kennt den Bucket der Datensicherungen nicht."
  }
}

# Kosten vor Verfügbarkeit (KO1): im Leerlauf keine Instanz, höchstens 2;
# Logs 30 Tage (P5).
run "kosten_und_aufbewahrung" {
  command = plan

  assert {
    condition     = google_cloud_run_v2_service.server.template[0].scaling[0].min_instance_count == 0
    error_message = "Der Server hält im Leerlauf Instanzen vor (kostet Geld)."
  }
  assert {
    condition     = google_cloud_run_v2_service.server.template[0].scaling[0].max_instance_count == 2
    error_message = "Der Server darf mehr als 2 Instanzen starten."
  }
  assert {
    condition     = google_logging_project_bucket_config.eu.retention_days == 30
    error_message = "Logs werden nicht 30 Tage aufbewahrt."
  }
}

# Rechte (Review Lauf 5, E2): Das Build-Konto liest keine Datensicherungen und
# schreibt Images nur ins Repo der Funktionen.
run "build_konto_ist_eng" {
  command = plan

  assert {
    # Genau diese Buckets, als eigene Liste geschrieben: Jeder weitere oder
    # breitere Eintrag (etwa alle Buckets) macht den Test rot.
    condition = google_project_iam_member.bauen_quellen.condition[0].expression == join(" || ", [
      "resource.name == \"projects/_/buckets/test-projekt-quellen\"",
      "resource.name.startsWith(\"projects/_/buckets/test-projekt-quellen/\")",
      "resource.name.startsWith(\"projects/_/buckets/gcf-v2-\")",
    ])
    error_message = "Das Build-Konto darf mehr Buckets lesen als den Quellen-Bucket und die von Cloud Functions."
  }
  assert {
    condition     = google_artifact_registry_repository_iam_member.bauen_funktionen.repository == "funktionen" && google_artifact_registry_repository_iam_member.bauen_funktionen.member == "serviceAccount:funktionen-bauen@test-projekt.iam.gserviceaccount.com"
    error_message = "Das Build-Konto schreibt Images nicht (nur) ins Repo der Funktionen."
  }
  assert {
    condition     = google_project_iam_member.bauen_quellen.member == "serviceAccount:funktionen-bauen@test-projekt.iam.gserviceaccount.com"
    error_message = "Das Leserecht auf die Quellen hat ein anderes Konto als das Build-Konto."
  }
}

run "lehnt_region_ausserhalb_der_eu_ab" {
  command = plan

  variables {
    region = "us-central1"
  }

  expect_failures = [var.region]
}

run "lehnt_europaeische_nicht_eu_region_ab" {
  command = plan

  variables {
    region = "europe-west2" # London: Europa, aber nicht EU
  }

  expect_failures = [var.region]
}

run "lehnt_budget_mit_nachkommastellen_ab" {
  command = plan

  variables {
    budget_eur = 20.5
  }

  expect_failures = [var.budget_eur]
}

run "lehnt_sperren_ueber_trennen_ab" {
  command = plan

  variables {
    schwelle_sperren = 2
    schwelle_trennen = 1.5
  }

  expect_failures = [var.schwelle_trennen]
}

# Fällt der Kill-Switch still aus, kommt eine Mail (Absenz-Bedingung auf seine
# Aufrufe, summiert über Revisionen).
run "alarm_bei_stillem_kill_switch" {
  command = plan

  assert {
    condition     = length(google_monitoring_alert_policy.killswitch_still.conditions[0].condition_absent) == 1
    error_message = "Es gibt keinen Alarm, wenn der Kill-Switch nicht mehr aufgerufen wird."
  }
  assert {
    condition     = strcontains(google_monitoring_alert_policy.killswitch_still.conditions[0].condition_absent[0].filter, "resource.labels.service_name=\"killswitch\"")
    error_message = "Der Absenz-Alarm beobachtet nicht den Kill-Switch."
  }
  assert {
    condition     = google_monitoring_alert_policy.killswitch_still.conditions[0].condition_absent[0].aggregations[0].cross_series_reducer == "REDUCE_SUM"
    error_message = "Ohne Summe über Revisionen meldet der Alarm nach jedem Deploy fälschlich Stille."
  }
}

# Pfeffer: Ohne ihn findet kein Gerät sein Konto, sein Schlüssel-Backup und seine
# Datensicherung wieder. prevent_destroy prüft OpenTofu selbst; hier nur, was
# außerhalb von OpenTofu schützt.
run "pfeffer_bleibt_wiederherstellbar" {
  command = plan

  assert {
    # KRITISCH: Eine zerstörte Version bleibt 30 Tage wiederherstellbar.
    condition     = google_secret_manager_secret.pfeffer.version_destroy_ttl == "2592000s"
    error_message = "Eine zerstörte Version des Pfeffers ist nicht 30 Tage wiederherstellbar."
  }
  assert {
    # Ein Ablaufdatum würde das Secret samt Pfeffer von selbst löschen.
    condition     = google_secret_manager_secret.pfeffer.ttl == null
    error_message = "Das Secret des Pfeffers läuft ab."
  }
  assert {
    condition     = tostring(google_secret_manager_secret_version.pfeffer.secret_data_wo_version) == "1"
    error_message = "Der Pfeffer bekäme einen neuen Wert; alle Konten wären unauffindbar."
  }
}
