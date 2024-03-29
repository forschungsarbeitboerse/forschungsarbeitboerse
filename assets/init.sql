CREATE TABLE IF NOT EXISTS postings (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	uuid TEXT NOT NULL UNIQUE,

	created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
	last_updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
	last_verified_at TIMESTAMP DEFAULT NULL,

	deleted INTEGER DEFAULT 0,
	verified INTEGER DEFAULT 0,

	admin_token TEXT NOT NULL UNIQUE,
	verify_token TEXT NOT NULL UNIQUE,

	email TEXT NOT NULL,

	title TEXT NOT NULL,
	institute TEXT NOT NULL,
	advisor TEXT DEFAULT "",
	supervisor TEXT DEFAULT "",
	audience TEXT DEFAULT "",
	category TEXT DEFAULT "",
	type TEXT DEFAULT "",
	degree TEXT DEFAULT "",
	start TEXT DEFAULT "",
	required_months INTEGER DEFAULT 0,
	required_effort TEXT DEFAULT "",
	text TEXT NOT NULL
);
