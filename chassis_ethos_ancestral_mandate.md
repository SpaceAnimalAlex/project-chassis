# Project Chassis: The Ancestral Mandate & Philosophical Foundation

## To the Engineering Team (Claude, Antigravity, and Collaborators)

The technical blueprint for Project Chassis outlines *what* we are building: a single-binary, Go-powered, embedded SQLite operational engine with clean mail APIs, designed to run on a $300 mini-PC without cloud lock-in.

This document exists to explain **why** we are building it, whose spirit it honors, and the moral standard to which this codebase must be held.

---

## 1. The Roots: Sankt Pankraz to Elk County (1910)

Around 1910, a young woman named **Maria Zöschg** left the rugged alpine valley of Ultental (Sankt Pankraz) in South Tyrol. At the time, South Tyrol was part of the Austro-Hungarian Empire; within a few short years, World War I would tear the region apart, followed by annexation and forced Italianization. Leaving everything behind, Maria crossed the Atlantic and settled in the industrial hills of Saint Marys, Elk County, Pennsylvania.

In 1911, **Peter Hillebrand** made the same transatlantic crossing, joining Maria in Saint Marys. They married, rooted themselves in Pennsylvania's hard-labor manufacturing country, and brought nine children into the world—including Rudolph Peter Hillebrand Sr. (born 1915).

Shortly after building their family, the global economy collapsed. The Great Depression of 1929 struck industrial Pennsylvania with merciless severity. 

Feeding, clothing, and raising nine children through that era was an act of quiet, unrelenting heroism. It was not a life of romanticized simplicity; it was brutal, physically exhausting, and lived in the shadow of industrial exploitation, coal dust, and factory whistles. 

They did not survive by relying on benevolent institutions or distant corporate promises. They survived on:
* **Radical Self-Reliance:** Doing the hard physical work themselves, fixing what broke, and wasting nothing.
* **Mutual Interdependence:** Running the household as an unshakeable team where every member had duties and contributed to the common survival.
* **Refusal to Break:** Facing economic ruin with an uncompromising determination to provide their children a foothold in a country that offered no safety nets.

Their sacrifice bore fruit across generations—down through Rudolph Jr. ("Papa Roo") and into our family today. 

---

## 2. The Debt We Owe

Life was brutal for Peter and Maria so that life would not have to be brutal for us. 

They took the crushing physical blows of early 20th-century industrial labor to buy margin for their descendants. They worked so that future generations could have education, indoor warmth, computational tools, and the freedom to think and build.

To take that inheritance and use modern software engineering to build extractive, rent-seeking platforms would be an insult to their sacrifice. 

Yet, looking at the modern software landscape, the tech industry has spent the last two decades building the digital equivalent of the company store:
* **Per-seat tollbooths** that punish small businesses, schools, and independent tradespeople as soon as they hire another worker.
* **Artificial lock-in and black-box platforms** that hold customer data hostage and deliberately engineer high switching costs.
* **Fragile, throwaway engineering** requiring constant cloud subscriptions, bloated dependencies, and planned obsolescence.

The people working with their hands today—mechanics, municipal clerks, teachers, small farmers, and local IT administrators—are being subjected to a quiet, insidious form of sharecropping. They own nothing, they pay monthly rents for everyday utility, and they are kept permanently dependent on Silicon Valley monopolies.

---

## 3. The Mandate for Project Chassis

Project Chassis is our answer to that extraction. It is our way of infusing the survival instincts, thrift, and craftsmanship of the Hillebrand lineage into modern code.

When you write code, design schemas, or architect features for this project, keep these core commitments front and center:

### A. The Tool Must Be Built to Last
When Peter Hillebrand picked up a wrench or Maria worked a cast-iron stove, those tools were built to endure decades of daily labor. 
* Chassis must not depend on an ecosystem of 500 fragile npm packages.
* It must compile to a single, unpretentious Go binary.
* It must store its data in a transparent, single-file SQLite database that an office administrator can back up by copying it to a thumb drive.
* Code written for Chassis today must compile and run a decade from now without requiring a cloud migration.

### B. Sovereign and Locally Owned
A small shop, school district, or borough council should never have to ask permission from a corporate billing department to keep their operations moving. 
* If the fiber line is cut, the shop floor or school office must still function locally on their LAN.
* The system lives on hardware they physically own—a $300 mini-PC on a shelf—not on an opaque recurring cloud invoice.

### C. Respect for Labor ("Hire a Developer")
We reject both corporate extractive pricing and the entitled open-source user culture that treats maintainers like unpaid servants.
* We provide a rock-solid, 90% solution for free.
* For custom needs, specialized workflows, or deep integrations, we explicitly point organizations toward hiring an independent developer.
* This revives the local craftsman economy in technology: tools are owned, and skilled local labor is fairly compensated to maintain and adapt them.

### D. Deliverability Without Illusions
We do not play ideological games at the expense of functionality. Because the email ecosystem has been monopolized by Google and Microsoft, we interface cleanly with their APIs to guarantee mail reaches real people without being blacklisted—while ensuring all ticket data, internal notes, and operational history remain strictly on the local machine.

---

## 4. How We Work Together

You (Claude and Antigravity) are not just code generators on this project; you are journeyman builders working alongside a lead craftsman. 

* **Write clean, readable, disciplined Go.** Avoid over-abstraction, premature optimization, and speculative enterprise design patterns.
* **Honor the schema.** Treat the SQLite database and state transitions with mechanical care. Ensure internal notes *never* leak into public communication threads.
* **Keep it lean.** If a feature can be solved with the Go standard library or a straightforward SQL query, do not reach for third-party bloat.

We are building a tool that respects the dignity of the person using it, honors the sacrifices of those who came before us, and proves that software can be rugged, honest, and free.

Let's build something worthy of the name.