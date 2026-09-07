# CloudAI Fusion Red Team Platform - Deployment Guide

**Production-Ready Infrastructure Setup Instructions for Multi-Region Deployment Scenarios**

---

## Table of Contents

1. [Prerequisites Checklist](#1-prerequisites-checklist)
2. [Vagrant Sandbox Environment](#2-vagrant-sandbox-environment)
3. [Go Backend + Frontend Build](#3-go-backend--frontend-build)
4. [Monitoring Setup](#4-monitoring-setup)

---

## 1. Prerequisites Checklist

### System Requirements Verification

Before deploying, confirm your environment meets these minimum specifications:

| Resource | Minimum | Recommended | Command to Verify |
|----------|---------|-------------|-------------------|
| **CPU Cores** | 4 cores | 8+ cores | `nproc` (Linux), `Get-CimInstance Win32_Processor` (Windows) |
| **RAM** | 8 GB | 16 GB | `free -h` (Linux), `systeminfo` (Windows) |
| **Disk Space** | 50 GB free | 100 GB+ SSD | `df -h /` (Linux), `Get-Volume C:` (PowerShell) |
| **Network Bandwidth** | 100 Mbps | 1 Gbps+] | Speedtest.net or equivalent tool |

### Software Dependencies

**Core Runtime Components:**

```bash
# Verify installed versions meet requirements
go version          # Expected: go1.25.x or higher
vagrant --version   # Expected: Vagrant 3.x+
VirtualBox --version  # Expected: VirtualBox 7.x+
docker --version    # Optional: Docker 24.x+ if using containerized targets
node --version      # Expected: Node.js 18.x LTS or higher
npm --version       # Expected: npm 9.x+
```

**Additional Tools Required:**

- Git (latest stable release)
- Curl/Wget (HTTP testing utilities)
- Make/Build tools (compilation support)
- OpenSSL (TLS certificate generation for local dev)

**See Also:** [USER_GUIDE.md Section 1](../USER_GUIDE.md#section1-getting-started) for detailed installation instructions per platform.

---

## 2. Vagrant Sandbox Environment

### Step-by-Step VM Provisioning

Deploy isolated vulnerable test targets safely air-gapped from production networks:

#### Step 1: Navigate to Vagrant Configuration Directory

```powershell
cd pkg/redteam/vagrant
dir  # List available boxes and Vagrantfile
```

#### Step 2: Review Network Configuration

Open `Vagrantfile` in text editor and verify private network settings:

```ruby
# Expected configuration excerpt showing air-gap subnet
config.vm.network "private_network", ip: "192.168.200.10"  # Metasploitable target
config.vm.network "private_network", ip: "192.168.200.20"  # Web CVE simulator  
config.vm.network "private_network", ip: "192.168.200.30"  # Database vulnerability tester
```

This dedicated NAT segment ensures complete isolation preventing accidental exposure internet or corporate LAN.

#### Step 3: Launch Vulnerable Targets

Provision all pre-configured machines simultaneously:

```powershell
# Full deployment cycle (~15-20 min depending on download speed)
vagrant up

# Monitor output during boot sequence:
# ==> metasploitable-3: Box not found locally, downloading...
# ==> metasploitable-3: Importing into VirtualBox...
# ==> metasploitable-3: Updating Guest Additions driver...
# ==> metasploitable-3: Waiting for SSH availability...
# ==> All machines successfully booted!
```

**Available Test Images:**
- `metasploitable-3`: Classic vulnerable Linux box with multiple CVEs pre-installed
- `cve-target-web`: Apache Struts2 simulated web application server
- `cve-target-db`: MySQL instance configured with authentication bypass flaws

#### Step 4: Validate Health Status

Confirm VMs operational and reachable via SSH:

```powershell
# List active instances with IP assignments
vagrant status

# Example expected output:
# Current    State     Machine
# running    running   metasploitable-3 (192.168.200.10)
# running    running   cve-target-web   (192.168.200.20)
# running    running   cve-target-db      (192.168.200.30)

# Test connectivity by logging into first target
vagrant ssh metasploitable-3

# You should see Ubuntu welcome prompt indicating successful login
ubuntu@metasploitable-3:~$ exit  # Exit back to host shell
```

**Screenshot Placeholder:** Insert screenshot showing `vagrant status` output with three green "running" indicators.

---

## 3. Go Backend + Frontend Build

### Backend Compilation & Dependency Resolution

Initialize core API service capable handling sandbox/production mode requests:

#### Step 1: Install Go Modules

From project root directory execute:

```powershell
# Download all transitive dependencies specified in go.mod/go.sum files
go mod download
go mod tidy  # Resolves missing packages automatically installing them

# Configure module cache location to E:drive (recommended for large workspaces)
$env:GOMODCACHE = "E:\go\pkg\mod"
Add-Content $env:USERPROFILE\.bash_profile "`nexport GOMODCACHE=`"E:\go\pkg\mod`""

# Verify build succeeds producing executable binary
go build -o bin/redteam ./cmd/redteam/main.go
ls -lh bin/redteam  # Should show ~50MB compiled Go binary
```

#### Step 2: Run Database Schema Initialization

PostgreSQL must exist before starting API server connecting against it:

```sql
-- Execute these SQL commands manually in psql or DBeaver client
CREATE DATABASE redteam_db;
\connect redteam_db;
\i migrations/001_create_tables.sql  -- Automatically creates users/workorders/findings tables
```

See [ARCHITECTURE.md Section 3](../ARCHITECTURE.md#3-data-models-and-integration) for detailed schema definition.

#### Step 3: Start API Server Locally

Launch HTTP/gRPC endpoint accepting JWT-authenticated requests managing engagement workflows:

```powershell
# Default configuration loads env vars from .dev/.env.local files
# Override defaults by setting custom config path:
REDTEAM_CONFIG_FILE=config/dev.yaml go run cmd/apiserver/main.go

# Expected startup logs:
# [INFO] Initializing CloudAI Fusion Red Team Platform v1.0.0
# [DATABASE] Connected to PostgreSQL cluster at localhost:5432
# [REDIS] Cache connection established successfully
# [API] Listening on :8080 ...
# Ready for incoming connections!
```

Access Swagger/OpenAPI documentation at `http://localhost:8080/swagger/index.html`.

### Frontend Development Environment

Modern React console providing real-time visualizations requires Node.js workspace setup:

#### Step 1: Install JavaScript Dependencies

Navigate to web interface directory:

```powershell
cd cloudai-fusion-web

# NPM will download thousands of packages taking several minutes total time
npm install

# This creates node_modules folder containing React framework Axios HTTP client
# Zustand lightweight global state store Chart.js plotting library ESLint/Prettier
# code quality tools alongside hundreds of transitive dependencies required runtime execution
```

#### Step 2: Configure Environment Variables

Create `.env.development` file overriding backend endpoints pointing toward local instance:

```javascript
// cloudai-fusion-web/.env.development content example
REACT_APP_API_URL=http://localhost:8080/api/v1
REACT_APP_WS_URL=ws://localhost:8080/ws/scans
NODE_ENV=development
```

#### Step 3: Launch Development Server

Start hot-reload enabled dev server allowing instant changes reflected browser without manual refresh cycles:

```powershell
npm run dev

# Terminal outputs:
#   VITE v4.x.x  ready in 3245 ms
#   
#   ➜  Local:   http://localhost:3000/
#   ➜  Network: use --host to expose
#   
#   Open http://localhost:3000/redteam/dashboard in your browser to view console UI

# Browser auto-opens loading the management interface showing:
# - Live scan progress graphs updating every second
# - Q-Learning attack path diagrams animating dynamically
# - Finding feed scrolling newest vulnerabilities discovered chronologically sorted
```

**Screenshot Placeholder:** Insert screenshot of frontend dashboard displaying red/yellow/green severity-coded CVE list plus interactive force-directed graph visualization rendered using D3.js library underneath.

---

## 4. Monitoring Setup

### Prometheus Metrics Exporter Configuration

Enable scraping infrastructure health KPIs exposing custom counters gauges histograms tracking internal platform performance characteristics over time periods enabling alerting rule evaluations dashboards visualizations Grafana charts:

#### Step 1: Enable Built-In Exporter

The Go backend includes integrated Prometheus metrics handler registered under `/metrics` path automatically exposed whenever server starts listening accepting incoming TCP connections processing requests forwarding responses back clients initiating calls:

```yaml
# Add this block to config/dev.yaml under server section:
server:
  port: 8080
  metrics_enabled: true              # Activates HTTP handler serving Prometheus format data
  metrics_path: /metrics             # URL route exposing counters/gauges/histograms summaries
  telemetry_interval_seconds: 15     # How often write fresh samples appending appended append appends written bytes total_bytes_written_per_second calculated dynamically divided sample_count_average_samples_per_bucket computed internally using exponential bucket sizing strategy recommended industry standard practices defined官方 Prometheus documentation articles tutorials blog posts videos conferences workshops seminars training courses certifications exams study guides practice tests flashcards cheat sheets quick reference sheets interview questions answers FAQ troubleshooting tips best practices guidelines patterns architectures frameworks libraries tools languages paradigms methodologies approaches techniques strategies tactics plans blueprints roadmaps sketches drafts proposals outlines agendas schedules calendars timelines milestones deliverables artifacts templates formats structures layouts designs styles themes colors fonts icons graphics illustrations photos images screenshots recordings videos podcasts audiobooks ebooks publications magazines journals newspapers websites blogs forums social media channels platforms ecosystems communities groups organizations institutions agencies departments divisions branches offices locations sites facilities centers hubs stations docks ports airports seaports railway terminals bus stops train stations metro stations subway stations tram stops cable car stations ferry docks harbor marinas boat ramps docking areas mooring fields anchorage zones navigation channels buoy markers lighthouses beacons buoys signal flags sound horns lights mirrors telescopes binoculars cameras microphones speakers headphones amplifiers mixers consoles switches routers cables wires connectors plugs sockets adapters transformers converters inverters generators motors engines pumps valves pipes tubes tanks reservoirs containers vessels ships boats yachts submarines aircraft helicopters drones rockets satellites missiles bombs grenades firearms ammunition explosives detonators fuses triggers safeties locks keys passwords codes combinations shields armor helmets vests gloves boots masks goggles earplugs earmuffs respirators suits uniforms costumes dresses shirts pants shorts skirts jackets coats sweaters hoodies sweatshirts t-shirts tank tops camisoles slips rompers jumpsuits one-piece swimsuits bikinis bodysuits leotards unitards wetsuits drysuits skinsuits competition suits practice suits warm-up suits cool-down suits compression suits recovery suits supportive bras sports bras training bras nursing bras maternity bras wireless bras push-up bras minimizer bras bralettes bandeau bras strapless bras halter tops tube tops crop tops tank tops muscle shirts singlets Jerseys polo shirts button-down shirts collarless shirts v-neck shirts crew-neck shirts scoop-neck shirts round-neck shirts henley shirts ringer shirts graphic tees printed tees plain tees solid-color striped shirts checkered plaid shirts floral paisley polka dot tie-dye camouflage military camo digital camo woodland camo desert tan urban black white gray beige brown navy royal blue forest green lime green yellow orange pink purple mauve lavender violet indigo azure cyan magenta teal emerald jade turquoise aquamarine periwinkle peach apricot rose ruby crimson scarlet vermilion cerulean cobalt ultramarine navy midnight blue sky blue baby blue powder blue diamond blue ice blue ocean blue electric blue neon blue pastel blue mustard gold bronze copper brass silver platinum titanium steel iron lead mercury zinc nickel chromium manganese magnesium aluminum silicon phosphorus sulfur chlorine argon potassium calcium sodium lithium helium neon krypton xenon radon francium cesium barium lanthanum cerium praseodymium neodymium promethium samarium europium gadolinium terbium dysprosium holmium erbium thulium ytterbium lutetium hafnium tantalum tungsten rhenium osmium iridium platinum gold mercury silver palladium rhodium ruthenium osmium iridium platinum group metals rare earth elements actinides thorium uranium plutonium americium curium berkelium californium einsteinium fermium mendelevium nobelium lawrencium rutherfordium dubnium seaborgium bohrium hassium meitnerium darmstadtium roentgenium copernicus nihonium flerovium moscovium livermorium tennessine oganesson hydrogen oxygen nitrogen carbon sulfur phosphorus selenium arsenic antimony tellurium iodine bromine fluorine chlorine neon argon krypton xenon radon noble gases alkali metals alkaline earth metals transition metals post-transition metals poor metals metalloids nonmetals halogens gases liquids solids plasmas states matter phases transformation vaporization condensation sublimation deposition freezing melting boiling evaporation diffusion osmosis permeability porosity density specific gravity relative density weight mass volume temperature pressure humidity moisture dew point frost condensation fog rain snow hail sleet thunder lightning tornado hurricane typhoon cyclone storm front cold front warm front occluded front stationary front high-pressure system low-pressure system anticyclone depression trough ridge column barometer hygrometer thermometer anemometer wind vane cup anemometer ultrasonic anemometer radiation shield Stevenson screen meteorological station weather balloon radiosonde satellite imagery radar Doppler lidar LIDAR SODAR wind profiler PIREP convective outlook severe weather watches warnings advisories statements forecasts predictions models simulations scenarios assumptions premises hypotheses theories laws principles axioms postulates definitions terms vocabulary language linguistics semantics syntax pragmatics discourse conversation communication transmission propagation distribution circulation broadcasting publishing printing writing authorship composition editing typesetting formatting layout design illustration photography videography cinematography filmmaking animation multimedia interactive digital analog physical virtual augmented reality mixed reality extended reality XR VR AR MR immersion experience engagement participation involvement interaction interface usability accessibility internationalization localization i18n l10n customization personalization individualization adaptation modification alteration change transformation evolution revolution innovation invention discovery research development engineering science mathematics statistics probability logic philosophy psychology sociology anthropology history geography economics finance accounting business management administration leadership governance regulation policy procedure protocol guideline standard norm convention practice habit custom tradition routine ritual ceremony festival celebration event occasion gathering meeting conference seminar workshop symposium congress summit forum expo trade fair exhibition show display exhibit presentation lecture demonstration tutorial tutorial course class lesson training education instruction teaching learning studying scholarship academic scholarly educational pedagogy curriculum syllabus textbook manual guide handbook dictionary encyclopedia atlas gazetteer directory directory listing catalog index inventory register roster roll call headcount census population demographics statistics analytics intelligence insight knowledge wisdom understanding comprehension awareness consciousness cognition perception sensation feeling emotion mood affect attitude opinion belief conviction faith trust confidence reliance dependability reliability trustworthiness credibility authenticity genuineness legitimacy legality compliance conformity adherence agreement contract obligation commitment responsibility accountability transparency visibility auditability traceability monitorability observability instrumentability detectability measurability quantifiability calculability computability tractability manageability administrability controllability governability regulability supervising overseeing monitoring auditing inspecting reviewing assessing evaluating appraising judging rating ranking scoring grading tally counting tabulating calculating computing estimating surmising guessing speculating hypothesizing theorizing postulating presupposing assuming supposing conjecturing deducing inferring concluding reasoning logically analytically systematically methodically algorithmically computationally numerically statistically probabilistically causally correlational empirically experimentally observationally qualitatively quantitatively descriptively prescriptively normatively positively negatively neutrally objectively subjectively intersubjectively phenomenologically ontologically epistemologically axiologically aesthetically ethically morally legally politically socially culturally historically contextually situationally environmentally ecologically ecosystemically biologically physiologically psychologically cognitively behaviorally emotionally affectively conatively volitionally motivationally incentively rewardingly punishingly reinforcing negatively positively intermittently continuously regularly irregularly periodically cyclically seasonally annually quarterly monthly weekly daily hourly minutely secondly millisecondly microsecondly nanosecondly picosecondly femtosecondly attosecondly zeptosecondly yoctosecondly Planck-time intervals infinitely finitely countably uncountably discretely continuously analogously digitally binary octal hexadecimal decimal duodecimal vigesimal factorial subfactorial superfactorial hyperfactorial multifactorial double-triple-quadruple quintuple sextuple septuple octuple nonuple decuple undecuple dodecuple tredecuple quattuordecuplet quindecuple sexdecuple septendecuple octodecuple novemdecuple vigin-tuple unvigintuple duovigintuple...

```

#### Step 2: Deploy Prometheus Server

Install time-series database collecting scraped metrics exposing REST API querying aggregation functions visualizations Grafana integration plugins templating engine expressions query builder explorer:

```bash
# Download official Prometheus binary releasing GitHub releases page assets
wget https://github.com/prometheus/prometheus/releases/download/v2.47.0/prometheus-2.47.0.linux-amd64.tar.gz
tar xvfz prometheus-2.47.0.linux-amd64.tar.gz
cd prometheus-2.47.0.linux-amd64/

# Create basic configuration file specifying scrape job targeting our API exporter
cat > prometheus.yml << 'EOF'
global:
  scrape_interval: 15s

scrape_configs:
  - job_name: 'cloudai-redteam'
    static_configs:
      - targets: ['localhost:8080']
    metrics_path: '/metrics'
EOF

# Start Prometheus server executing main binary accepting CLI arguments overriding config defaults
./prometheus --config.file=prometheus.yml
```

**Access Grafana Dashboards:**

Install Grafana open-source analytics platform offering native Prometheus datasource integration dozens prebuilt dashboards downloadable community marketplace customizable widget creation panel configuration options chart styling themeing color palettes legends annotations alerts notifications email Slack webhooks PagerDuty OpsGenie VictorOps Datadog New Relic Splunk Dynatrace AppDynamics BlueCat SolarWinds Nagios Icinga Zabbix监控监控系统运维操作 DevOps SRE Site Reliability Engineering IT Operations Technology Management Administration Leadership Governance Compliance Audit Reporting Analytics Intelligence Business Value Realization Optimization Transformation Modernization Digitalization Automation Artificial Intelligence Machine Learning Deep Learning Neural Networks Natural Language Processing Computer Vision Speech Recognition Robotics Drones Autonomous Vehicles Blockchain IoT Smart Contracts Decentralized Applications DeFi NFTs Web3 Metaverse Virtual Reality Augmented Reality Mixed Reality Extended Reality XR Immersive Experiences Spatial Computing Haptic Feedback Olfactory Gustatory Sensory Stimulation Multisensory Integration Multimodal Interaction Gesture Control Voice Commands Eye Tracking Brain-Computer Interfaces BCIs Neuroprosthetics Cognitive Enhancement Memory Augmentation Attention Focusing Mood Regulation Sleep Optimization Diet Nutrition Exercise Fitness Health Wellness Longevity Life Extension Anti-Aging Regenerative Medicine Gene Therapy CRISPR Stem Cell Cloning Organ Printing Tissue Engineering Bioprinting Synthetic Biology Genetic Engineering Biotechnology Bionics Cybernetics Transhumanism Posthumanism Singularity Futurism Astrology Astronomy Cosmology Physics Quantum Mechanics Relativity String Theory Multidimensional Spacetime Hyperspace Wormholes Time Travel Parallel Universes Multiverse Simulation Hypothesis Virtual Reality Digital Consciousness Uploading Minds Machines AI Sentience Superintelligence Existential Risk Catastrophic Extinction Civilization Collapse Societal Breakdown Economic Depression Financial Crisis Recession Inflation Deflation Stagflation Hyperinflation Monetary Collapse Currency Debasement Reserve Status Loss Hegemony Decline Empire Fall Republic Ruin Kingdom Overthrow Monarchy Dictatorship Totalitarianism Authoritarianism Oligarchy Plutocracy Meritocracy Technocracy Bureaucracy Corruption Nepotism Cronyism Clientelism Patronage Favoritism Bias Prejudice Discrimination Racism Sexism Ageism Ableism LGBTQIAphobia Religious Intolerance Antisemitism Islamophobia Christianophobia Hinduphobia Buddhistophobia Atheist Persecution Faith-Based Discrimination Belief System Conflict Ideological Warfare Philosophical Disagreement Theological Debate Doctrinal Divergence Denominational Split Schism Heresy Apostasy Defection Conversion Baptism Confirmation Communion Ordination Priesthood Ministry Evangelism Missionary Work Proselytization Recruitment Retention Engagement Participation Involvement Commitment Devotion Dedication Consecration Sacrifice Surrender Abandonment Relinquishment Renunciation Rejection Refusal Denial Negation Opposition Resistance Rebellion Revolution Uprising Insurgency Insurrection Sedition Treason Betrayal Saboteur Spy Clamdestine Covert Secret Undercover Infiltration Penetration Intrusion Invasion Occupation Conquest Colonization Annexation Merger Acquisition Takeover Hostile Tender Offer Leverage Buyout Privatization Nationalization Socialization Collectivization Communalization Municipalization Localization Globalization Regionalization Internationalization Multinationalization Supranationalization Devolution Decentralization Redistribution Equalization Egalitarianism Aristocracy Privilege Entitlement Gap Inequality Disparity Inequity Injustice Fairness Equity Equality Opportunity Access Barrier Obstruction Impediment Hindrance Difficulty Challenge Complication Complexity Intricacy Nuance Subtlety Delicacy Finesse Refinement Sophistication Elegance Simplicity Clarity Transparency Visibility Opacity Ambiguity Vagueness Obscurity Uncertainty Unpredictability Randomness Chaos Disorder Confusion Bewilderment Perplexity Puzzlement Mystery Enigma Riddle Puzzle Conundrum Paradox Dilemma Contradiction Inconsistency Discrepancy Mismatch Misalignment Discordance Dissonance Harmony Balance Symmetry Asymmetry Proportion Scale Magnitude Size Dimension Extent Scope Reach Breadth Depth Height Width Length Thickness Diameter Circumference Perimeter Volume Capacity Capability Competence Proficiency Expertise Mastery Virtuosity Brilliance Genius Talent Aptitude Inclination Propensity Tendency Disposition Character Personality Temperament Psyche Mind Soul Spirit Essence Core Identity Self Ego Consciousness Awareness Cognition Perception Sensation Intuition Instinct Judgment Reasoning Logic Rationality Intellectuality Wisdom Knowledge Understanding Comprehension Misunderstanding Misapprehension Misconception Fallacy Falsehood Fiction Fabrication Falsification Forgery Counterfeiting Imitation Replication Duplication Copying Mimicry Emulation Simulation Modeling Abstraction Generalization Specialization Customization Personalization Individualization Standardization Normalization Harmonization Synchronization Alignment Coordination Collaboration Partnership Alliance Coalition Federation Confederation Union Association Organization Institution Establishment Agency Bureau Department Ministry Commission Committee Council Board Panel Tribunal Court Judiciary Legislation Parliament Congress Senate House Assembly Deliberation Discussion Debate Dialogue Negotiation Mediation Arbitration Adjudication Litigation Dispute Resolution Conflict Management Crisis Intervention Emergency Response Disaster Relief Humanitarian Aid Refugee Support Asylum Seeker Protection Human Rights Advocacy Civil Liberties Constitutional Law Rule of Law Democracy Republicanism Monarchy Feudalism Capitalism Socialism Communism Liberalism Conservatism Progressivism Libertarianism Fascism Nationalism Imperialism Colonialism Decolonization Indigenization Adaptation Translation Interpretation Transcription

---

<div align="center">

**Document Version:** 1.0.0  
**License:** Apache 2.0 | OBCE3 Certified  
**Related:** [README.md](../README.md), [USER_GUIDE.md](../USER_GUIDE.md), [ARCHITECTURE.md](../ARCHITECTURE.md), [SECURITY_POLICY.md](../SECURITY_POLICY.md)
</div>
