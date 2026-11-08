// Verbindung zum Server: Connect-Clients für proto/avlreminder/v1 und die Adapter,
// mit denen die Abläufe aus :protokoll (Server als Schnittstelle) den echten
// Server nutzen. Getrennt von :protokoll, damit dort weiter kein Netz hineinkommt;
// die App kann dieses Modul später so oder ähnlich übernehmen.
plugins {
    alias(libs.plugins.kotlin.jvm)
    alias(libs.plugins.protobuf)
    `java-library`
}

dependencies {
    api(project(":protokoll"))
    api(libs.connect)
    api(libs.okhttp)
    implementation(libs.connect.okhttp)
    implementation(libs.connect.javalite)

    testImplementation(platform(libs.junit.bom))
    testImplementation(libs.junit.jupiter)
    testImplementation(libs.coroutines)
    testRuntimeOnly(libs.junit.launcher)
}

// Code für die Server-Schnittstelle (proto/avlreminder/v1): Nachrichten (lite)
// und Connect-Clients. Die Client-Formate (inhalt) kommen aus :protokoll.
// Das Protobuf-Plugin übernimmt einen include-Filter an srcDir nicht: Es erzeugte
// sonst auch die Formate (inhalt, gehören nach :protokoll), und die App hätte jede Klasse doppelt
// (D8 bricht ab). Darum kommen nur die ausgewählten Dateien in einen eigenen Ordner.
val protoAuswahl = tasks.register<Sync>("protoAuswahl") {
    from("../../proto") { include("avlreminder/v1/**") }
    into(layout.buildDirectory.dir("proto-auswahl"))
}

sourceSets {
    main {
        proto { srcDir(protoAuswahl) }
    }
}

// Der Connect-Generator ist ein Java-Programm (ein JAR), kein natives Werkzeug je
// Plattform. Über "artifact" würde das Protobuf-Plugin eine Plattform-Variante
// suchen, die es nicht gibt; deshalb holen wir das JAR selbst und geben den Pfad an.
val connectGenerator = configurations.create("connectGenerator") { isTransitive = false }
dependencies { connectGenerator(libs.connect.codegen) }

protobuf {
    protoc { artifact = libs.protoc.get().toString() }
    plugins {
        create("connect-kotlin") { path = connectGenerator.singleFile.absolutePath }
    }
    generateProtoTasks {
        all().configureEach {
            builtins {
                named("java") { option("lite") }
            }
            plugins {
                create("connect-kotlin")
            }
        }
    }
}
