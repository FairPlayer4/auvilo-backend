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
  assert {
    condition     = upper(google_storage_bucket.diagnose.location) == upper(var.region)
    error_message = "Der Bucket der Diagnosen liegt nicht in var.region."
  }
  assert {
    condition     = google_cloud_run_v2_job.aufraeumen.location == var.region
    error_message = "Der Job aufraeumen liegt nicht in var.region."
  }
  assert {
    condition     = google_cloud_scheduler_job.aufraeumen.region == var.region
    error_message = "Der Scheduler-Job liegt nicht in var.region."
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
  assert {
    condition     = upper(google_storage_bucket.diagnose.location) == "EUROPE-WEST10"
    error_message = "Der Bucket der Diagnosen folgt var.region nicht."
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

# Sendet der Server ungewöhnlich viel (viele Konten holen Sicherungen), kommt
# eine Mail, bevor das Budget sich meldet (Review-Lauf 8, Frage 3).
run "alarm_bei_viel_ausgehendem_verkehr" {
  command = plan

  assert {
    condition     = strcontains(google_monitoring_alert_policy.egress.conditions[0].condition_threshold[0].filter, "sent_bytes_count")
    error_message = "Der Egress-Alarm beobachtet nicht die gesendeten Bytes."
  }
  assert {
    condition     = strcontains(google_monitoring_alert_policy.egress.conditions[0].condition_threshold[0].filter, "resource.labels.service_name=\"server\"")
    error_message = "Der Egress-Alarm beobachtet nicht den Server."
  }
  assert {
    # Ein Filter auf kind="internet" könnte Antworten über Googles Frontend übersehen.
    condition     = !strcontains(google_monitoring_alert_policy.egress.conditions[0].condition_threshold[0].filter, "metric.labels.kind")
    error_message = "Der Egress-Alarm zählt nicht alle gesendeten Bytes."
  }
  assert {
    condition     = google_monitoring_alert_policy.egress.conditions[0].condition_threshold[0].aggregations[0].cross_series_reducer == "REDUCE_SUM"
    error_message = "Ohne Summe über Revisionen verteilt sich der Verkehr auf mehrere Zeitreihen."
  }
  assert {
    condition     = google_monitoring_alert_policy.egress.conditions[0].condition_threshold[0].threshold_value <= 1073741824
    error_message = "Die Schwelle des Egress-Alarms liegt über 1 GiB."
  }
}

# ID8: Eine Flut von Anfragen sperrt den Server über den Kill-Switch, lange
# bevor das Budget sie bemerkt.
run "kill_switch_bei_vielen_anfragen" {
  command = plan

  assert {
    condition     = strcontains(google_monitoring_alert_policy.anfragen.conditions[0].condition_threshold[0].filter, "request_count") && strcontains(google_monitoring_alert_policy.anfragen.conditions[0].condition_threshold[0].filter, "resource.labels.service_name=\"server\"")
    error_message = "Der Anfrage-Alarm zählt nicht die Anfragen an den Server."
  }
  assert {
    condition     = google_monitoring_alert_policy.anfragen.conditions[0].condition_threshold[0].threshold_value <= 10000 && google_monitoring_alert_policy.anfragen.conditions[0].condition_threshold[0].aggregations[0].alignment_period == "3600s"
    error_message = "Der Anfrage-Alarm schlägt erst über 10.000 Anfragen je Stunde an."
  }
  assert {
    condition     = google_monitoring_alert_policy.anfragen.conditions[0].condition_threshold[0].aggregations[0].cross_series_reducer == "REDUCE_SUM"
    error_message = "Ohne Summe über Revisionen verteilen sich die Anfragen auf mehrere Zeitreihen."
  }
  assert {
    # KRITISCH: Ohne dieses Label sperrt der Kill-Switch nicht.
    condition     = google_monitoring_alert_policy.anfragen.user_labels["killswitch"] == "sperren"
    error_message = "Der Anfrage-Alarm sagt dem Kill-Switch nicht, dass er sperren soll."
  }
  assert {
    condition     = google_monitoring_notification_channel.killswitch.type == "pubsub" && length(google_monitoring_alert_policy.anfragen.notification_channels) == 2
    error_message = "Der Anfrage-Alarm geht nicht an den Kill-Switch und per Mail."
  }
  assert {
    # KRITISCH: Eventarc wiederholt nicht; nur die erneute Meldung sperrt nach
    # einem gescheiterten ersten Versuch doch noch.
    condition     = google_monitoring_alert_policy.anfragen.alert_strategy[0].notification_channel_strategy[0].renotify_interval == "1800s" && one(google_monitoring_alert_policy.anfragen.alert_strategy[0].notification_channel_strategy[0].notification_channel_names) == google_monitoring_notification_channel.killswitch.id
    error_message = "Der Anfrage-Alarm meldet sich bei offenem Vorfall nicht alle 30 Minuten erneut beim Kill-Switch."
  }
  assert {
    condition     = google_pubsub_topic_iam_member.monitoring_meldet.topic == "budget-meldungen" && google_pubsub_topic_iam_member.monitoring_meldet.role == "roles/pubsub.publisher" && google_pubsub_topic_iam_member.monitoring_meldet.member == "serviceAccount:service-123456789@gcp-sa-monitoring-notification.iam.gserviceaccount.com"
    error_message = "Cloud Monitoring darf nicht im Thema des Kill-Switch veröffentlichen."
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
    # KRITISCH: Der Server darf den Pfeffer nur lesen, nicht löschen oder ändern.
    condition     = google_secret_manager_secret_iam_member.server_pfeffer.role == "roles/secretmanager.secretAccessor"
    error_message = "Der Server hat am Pfeffer mehr als Leserechte."
  }
  assert {
    condition     = google_secret_manager_secret_iam_member.server_pfeffer.member == google_service_account.server.member
    error_message = "Das Leserecht am Pfeffer hat nicht das Server-Konto."
  }
  assert {
    # KRITISCH: Der Server liest fest Version 1. "latest" nähme eine später
    # hinzugefügte Version, also einen anderen Pfeffer.
    condition = one([
      for e in google_cloud_run_v2_service.server.template[0].containers[0].env : e.value if e.name == "PFEFFER_SECRET"
    ]) == "${google_secret_manager_secret.pfeffer.name}/versions/1"
    error_message = "Der Server liest nicht fest Version 1 des Pfeffers."
  }
}

# Diagnosen (docs/diagnose-entwurf.md): nie öffentlich, nach 14 Tagen weg, der
# Server darf nur anlegen, freigeschaltet ist ohne Eintrag niemand.
run "diagnosen_sind_geschuetzt_und_kurzlebig" {
  command = plan

  # Ausdrücklich leer: Sonst nähme der Test lokal die Liste aus
  # werte.auto.tfvars und schlüge fehl (in der CI gibt es die Datei nicht).
  variables {
    diagnose_konten        = []
    freigegebene_kennungen = []
  }

  assert {
    condition     = google_storage_bucket.diagnose.public_access_prevention == "enforced" && google_storage_bucket.diagnose.uniform_bucket_level_access
    error_message = "Der Bucket der Diagnosen könnte öffentlich werden."
  }
  assert {
    # KRITISCH: genau eine Regel, die alles nach 14 Tagen löscht (E-D-5).
    condition     = length(google_storage_bucket.diagnose.lifecycle_rule) == 1 && one(google_storage_bucket.diagnose.lifecycle_rule[0].action).type == "Delete" && one(google_storage_bucket.diagnose.lifecycle_rule[0].condition).age == 14
    error_message = "Diagnosen werden nicht nach 14 Tagen gelöscht."
  }
  assert {
    condition     = google_storage_bucket.diagnose.soft_delete_policy[0].retention_duration_seconds == 0
    error_message = "Gelöschte Diagnosen bleiben im Soft Delete liegen."
  }
  assert {
    # objectCreator: anlegen ja, lesen, überschreiben, löschen nein.
    condition     = google_storage_bucket_iam_member.server_diagnose.member == "serviceAccount:server@test-projekt.iam.gserviceaccount.com" && google_storage_bucket_iam_member.server_diagnose.role == "roles/storage.objectCreator"
    error_message = "Das Server-Konto hat am Bucket der Diagnosen andere Rechte als nur Anlegen."
  }
  assert {
    condition     = contains([for e in google_cloud_run_v2_service.server.template[0].containers[0].env : e.value if e.name == "DIAGNOSE_BUCKET"], "test-projekt-diagnose")
    error_message = "Der Server kennt den Bucket der Diagnosen nicht."
  }
  assert {
    condition     = [for e in google_cloud_run_v2_service.server.template[0].containers[0].env : e.value if e.name == "DIAGNOSE_KONTEN"] == [""]
    error_message = "Ohne Eintrag in diagnose_konten ist jemand freigeschaltet."
  }
  assert {
    # KRITISCH (ID7): Ohne Eintrag lässt der Server niemanden zu.
    condition     = [for e in google_cloud_run_v2_service.server.template[0].containers[0].env : e.value if e.name == "FREIGEGEBENE_KENNUNGEN"] == [""]
    error_message = "Ohne Eintrag in freigegebene_kennungen ist jemand freigegeben."
  }
  assert {
    condition     = google_firestore_field.kontingente_ttl.collection == "kontingente" && google_firestore_field.kontingente_ttl.field == "laeuft_ab" && length(google_firestore_field.kontingente_ttl.ttl_config) == 1
    error_message = "Firestore löscht die Tageszähler nicht."
  }
}

run "gibt_freigeschaltete_konten_an_den_server" {
  command = plan

  variables {
    diagnose_konten = ["0123456789abcdef0123456789abcdef", "fedcba9876543210fedcba9876543210"]
  }

  assert {
    condition     = [for e in google_cloud_run_v2_service.server.template[0].containers[0].env : e.value if e.name == "DIAGNOSE_KONTEN"] == ["0123456789abcdef0123456789abcdef,fedcba9876543210fedcba9876543210"]
    error_message = "Der Server bekommt die freigeschalteten Konten nicht."
  }
}

run "lehnt_konto_id_mit_tippfehler_ab" {
  command = plan

  variables {
    diagnose_konten = ["0123456789ABCDEF0123456789abcdef"]
  }

  expect_failures = [var.diagnose_konten]
}

run "gibt_freigegebene_kennungen_an_den_server" {
  command = plan

  variables {
    freigegebene_kennungen = ["0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef", "fedcba9876543210fedcba9876543210fedcba9876543210fedcba9876543210"]
  }

  assert {
    condition     = [for e in google_cloud_run_v2_service.server.template[0].containers[0].env : e.value if e.name == "FREIGEGEBENE_KENNUNGEN"] == ["0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef,fedcba9876543210fedcba9876543210fedcba9876543210fedcba9876543210"]
    error_message = "Der Server bekommt die freigegebenen Kennungen nicht."
  }
}

run "stern_gibt_alle_frei" {
  command = plan

  variables {
    freigegebene_kennungen = ["*"]
  }

  assert {
    condition     = [for e in google_cloud_run_v2_service.server.template[0].containers[0].env : e.value if e.name == "FREIGEGEBENE_KENNUNGEN"] == ["*"]
    error_message = "Der Server bekommt den Stern nicht."
  }
}

run "lehnt_kennung_mit_tippfehler_ab" {
  command = plan

  variables {
    freigegebene_kennungen = ["0123456789ABCDEF0123456789ABCDEF0123456789ABCDEF0123456789ABCDEF"]
  }

  expect_failures = [var.freigegebene_kennungen]
}

run "lehnt_konto_id_statt_kennung_ab" {
  command = plan

  variables {
    freigegebene_kennungen = ["0123456789abcdef0123456789abcdef"]
  }

  expect_failures = [var.freigegebene_kennungen]
}

run "lehnt_stern_neben_kennungen_ab" {
  command = plan

  variables {
    freigegebene_kennungen = ["*", "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"]
  }

  expect_failures = [var.freigegebene_kennungen]
}

run "lehnt_zu_kurze_konto_id_ab" {
  command = plan

  variables {
    diagnose_konten = ["0123456789abcdef"]
  }

  expect_failures = [var.diagnose_konten]
}

# Vor echten Daten (b2b-entwurf.md): Datenbank, Sicherungen und Server lassen
# sich nicht mit `tofu destroy` löschen. In GCP selbst (Konsole, gcloud) schützt
# nur die Datenbank (delete_protection_state); deletion_protection am Dienst und
# force_destroy am Bucket wirken nur in OpenTofu.
run "echte_daten_sind_vor_dem_loeschen_geschuetzt" {
  command = plan

  assert {
    condition     = google_firestore_database.standard.delete_protection_state == "DELETE_PROTECTION_ENABLED"
    error_message = "Die Datenbank lässt sich in GCP löschen."
  }
  assert {
    # ABANDON: `tofu destroy` vergisst die Datenbank nur, statt sie zu löschen.
    condition     = google_firestore_database.standard.deletion_policy == "ABANDON"
    error_message = "`tofu destroy` würde die Datenbank löschen."
  }
  assert {
    condition     = !google_storage_bucket.datensicherung.force_destroy
    error_message = "`tofu destroy` würde den Bucket samt Sicherungen löschen."
  }
  assert {
    condition     = google_cloud_run_v2_service.server.deletion_protection
    error_message = "`tofu destroy` würde den Server-Dienst löschen."
  }
}

# Der Kill-Switch sperrt ohne actAs (killswitch/sperren.go): Er darf die
# IAM-Richtlinie setzen, aber keinen Dienst ändern und damit kein Image starten.
run "kill_switch_kann_keine_dienste_aendern" {
  command = plan

  assert {
    condition     = contains(google_project_iam_custom_role.killswitch_cloud_run.permissions, "run.services.setIamPolicy")
    error_message = "Der Kill-Switch kann keine Dienste sperren."
  }
  assert {
    # KRITISCH: Mit update (und actAs) könnte er ein fremdes Image als Konto
    # "server" starten.
    condition     = length([for p in google_project_iam_custom_role.killswitch_cloud_run.permissions : p if !startswith(p, "run.services.") || endswith(p, ".update") || endswith(p, ".create") || endswith(p, ".delete")]) == 0
    error_message = "Der Kill-Switch darf mehr als Dienste auflisten und ihre Richtlinie setzen."
  }
  assert {
    # Auch keine weitere Rolle neben der eigenen (z. B. run.admin oder
    # iam.serviceAccountUser = actAs).
    condition     = toset(keys(google_project_iam_member.killswitch)) == toset(["cloud_run", "abrechnung", "eventarc"]) && google_project_iam_member.killswitch["abrechnung"].role == "roles/billing.projectManager" && google_project_iam_member.killswitch["eventarc"].role == "roles/eventarc.eventReceiver"
    error_message = "Das Konto des Kill-Switch hat andere Rollen im Projekt als die eigene, Abrechnung trennen und Ereignisse empfangen."
  }
  assert {
    condition     = length([for m in google_project_iam_member.killswitch : m if m.member != google_service_account.killswitch.member]) == 0
    error_message = "Die Rollen des Kill-Switch gehen an ein anderes Konto."
  }
}

# Konto löschen (aufraeumen.tf): Der tägliche Lauf und der Server dürfen in den
# Buckets löschen, aber nichts lesen; der Lauf hat nur sein eigenes Konto.
run "aufraeumen_loescht_ohne_zu_lesen" {
  command = plan

  assert {
    # KRITISCH: Ohne storage.objects.get liest die Rolle keine Inhalte.
    condition     = toset(google_project_iam_custom_role.objekte_loeschen.permissions) == toset(["storage.objects.list", "storage.objects.delete"])
    error_message = "Die Rolle zum Löschen hat andere Rechte als Auflisten und Löschen."
  }
  assert {
    condition     = toset(keys(google_storage_bucket_iam_member.aufraeumen)) == toset(["datensicherung", "diagnose"]) && alltrue([for m in google_storage_bucket_iam_member.aufraeumen : m.role == google_project_iam_custom_role.objekte_loeschen.name && m.member == google_service_account.aufraeumen.member])
    error_message = "Der Lauf hat in den Buckets andere Rechte als Auflisten und Löschen."
  }
  assert {
    condition     = google_storage_bucket_iam_member.server_diagnose_loeschen.role == google_project_iam_custom_role.objekte_loeschen.name && google_storage_bucket_iam_member.server_diagnose_loeschen.bucket == "test-projekt-diagnose"
    error_message = "Der Server darf Diagnosen nicht oder mit zu viel Rechten löschen."
  }
  assert {
    condition     = google_cloud_run_v2_job.aufraeumen.template[0].template[0].service_account == google_service_account.aufraeumen.email
    error_message = "Der Job läuft nicht mit seinem eigenen Konto."
  }
  assert {
    condition     = google_cloud_run_v2_job.aufraeumen.template[0].template[0].containers[0].args == tolist(["aufraeumen"])
    error_message = "Der Job startet nicht den Unterbefehl aufraeumen."
  }
  assert {
    # Ohne Pfeffer: Der Lauf braucht keine Google-Konto-IDs.
    condition     = length([for e in google_cloud_run_v2_job.aufraeumen.template[0].template[0].containers[0].env : e if e.name == "PFEFFER_SECRET"]) == 0
    error_message = "Der Job bekommt den Pfeffer."
  }
  assert {
    condition     = google_cloud_run_v2_job_iam_member.planer_aufraeumen.role == "roles/run.invoker" && google_cloud_scheduler_job.aufraeumen.http_target[0].oauth_token[0].service_account_email == google_service_account.planer.email
    error_message = "Der Scheduler startet den Job nicht mit seinem eigenen Konto."
  }
}

# KO3: Ohne ausdrücklichen Wunsch öffnet `tofu apply` keinen gesperrten Dienst
# (module/oeffentlich). Wäre der Standard true, bliebe der Modultest grün.
run "gesperrte_dienste_bleiben_ohne_wunsch_zu" {
  command = plan

  assert {
    condition     = !var.gesperrte_dienste_oeffnen
    error_message = "tofu apply öffnet gesperrte Dienste ohne -var=gesperrte_dienste_oeffnen=true."
  }
}
