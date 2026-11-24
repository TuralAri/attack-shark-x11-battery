# Attack Shark X11 — Battery Tray

Utilitaire Windows ultra-léger en **Go pur** (zéro dépendance externe) pour afficher en temps réel le pourcentage de batterie de la souris **Attack Shark X11** branchée via son **dongle USB 2,4 GHz** (ou en filaire) directement dans la barre des tâches.

## Fonctionnalités

- **Icône dynamique dans le System Tray** : pourcentage lisible + barre de couleur (vert / orange / rouge / cyan en charge) adaptée automatiquement au thème clair ou sombre de Windows.
- **Détection automatique & reconnexion** : écoute passive des rapports HID du dongle (`VID 0x1D57`, `PID 0xFA60` / `0xFA55` / `0x22A9`, `Usage Page 0x000A`). Aucun logiciel constructeur requis.
- **Gestion de la mise en veille** : conserve le dernier pourcentage connu avec l'état *(en veille)* si la souris cesse d'émettre.
- **Notification batterie faible** : alerte Windows automatique sous **15 %**.
- **Démarrage avec Windows** : activable en un clic depuis le menu contextuel de l'icône (clé registre utilisateur `HKCU\...\Run`).
- **Instance unique** : empêche les doublons dans la barre des tâches.
- **Mode CLI** : lecture ponctuelle ou brute dans le terminal (`--once`, `--raw`).

## Prérequis

- Windows 10 / 11
- Go 1.22+ (uniquement pour compiler)

## Compilation

```powershell
.\build.ps1
```

Cela génère :
- `bin\attack-shark-battery.exe` — application barre des tâches (sans fenêtre console)
- `bin\attack-shark-battery-cli.exe` — version console pour tests rapides

## Utilisation

### Barre des tâches (recommandé)
Lance simplement `bin\attack-shark-battery.exe` (double-clic).  
Clic droit (ou clic gauche) sur l'icône pour :
- Voir l'état détaillé et l'heure de la dernière lecture
- Forcer la reconnexion au dongle
- Activer / désactiver **Démarrer avec Windows**
- Quitter

### Terminal (diagnostic)
```powershell
# Lecture rapide puis sortie
.\bin\attack-shark-battery-cli.exe --once

# Flux continu des paquets HID
.\bin\attack-shark-battery-cli.exe --raw
```

## Tests

```powershell
go test ./...
```

## Protocole HID utilisé

Le dongle 2,4 GHz (`1D57:FA60`) publie périodiquement un rapport d'entrée HID de 5 octets sur la collection `Usage Page 0x000A` :

```text
03 <device_id> <40|41> <état> <pourcentage 0x00..0x64>
```

| Octet | Description |
|-------|-------------|
| `0` | Report ID (`0x03`) |
| `1` | Identifiant modèle/appareil |
| `2` | Commande batterie (`0x40` ou `0x41`) |
| `3` | État : `0x00` veille, `0x01` sur batterie, `0x02` chargée, `0x03` en charge |
| `4` | Pourcentage brut (`0` à `100`) |
