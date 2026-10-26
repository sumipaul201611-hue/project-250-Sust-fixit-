# SUST FixIt

SUST FixIt is a web-based platform for reporting and tracking problems on the SUST campus. Students can report issues such as broken fans, poor cleanliness, food quality problems, and Wi-Fi issues. Other students can view reports and upvote problems that need attention.

The main goal of this project is to make campus problems easier to report and follow up on.

## Features

- **Student Login and Registration:** Students can create an account and log in.
- **Report a Problem:** Students can submit a report with a title, description, building, location, category, and an optional photo.
- **Similar Report Warning:** The system can show similar reports before a student submits a new one.
- **Public Issue Feed:** Students can view reports submitted by others.
- **Upvote Reports:** Students can upvote an existing report to show support for it.
- **Admin Panel:** Admins can view reports and update their status.
- **Status Tracking:** Students can follow a report as it moves from Reported to Verified, In Progress, and Resolved.
- **Resolution Photo:** Admins can add an after-photo when marking a report as resolved.

## Technologies Used

- **Frontend:** HTML, CSS, JavaScript
- **Backend:** Go (Golang)
- **Database:** SQLite
- **Authentication:** JWT

## Project Structure

```text
sust-fixit/
├── backend/
│   ├── main.go
│   ├── reports.go
│   └── ...
├── frontend/
│   ├── index.html
│   ├── report.html
│   ├── feed.html
│   ├── admin.html
│   ├── css/
│   └── js/
└── README.md
```

## How to Run

1. Install Go on your computer.
2. Open a terminal in the backend folder.
3. Run the following command:
   ```powershell
   go run .
   ```
4. Open your browser and visit:
   `http://localhost:8080`

Make sure the backend starts successfully before opening the website.

## Project Purpose

SUST FixIt was developed as a web technology project to help students report campus problems and make it easier to track their progress until they are resolved.
