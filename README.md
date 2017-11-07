# Minimal

Minimal is a static personal website and blog generator for Go, Node.js and Python. It has no external dependencies and requires only a few hundred lines of code to run. Everything is minimal, easy to take appart and rewrite.

Example blogs are hosted on Netlify using the [default](https://minimal-default.netlify.com), [profile](https://minimal-profile.netlify.com) and [developer](https://minimal-developer.netlify.com) themes.

## Getting Started

To get started, [fork](https://help.github.com/articles/fork-a-repo) this repository and create a local [clone](https://help.github.com/articles/cloning-a-repository).

Modify `./content.json` to your liking (symbol codes for social links can be found [here](http://drinchev.github.io/monosocialiconsfont)). 

To build locally and launch a simple web server run **either** of the following: 

* Install [Node.js](https://nodejs.org/en/download) and run `./task start --runtime node`.
* Install [Go](https://golang.org/doc/install) and run `./task start --runtime go`.
* Install [Python](https://www.python.org/downloads/) and run `./task start --runtime python`.

The default `runtime` can be configured via `./task.cfg`.

## Deployment

To deploy to a production enviroment set the deploy `target` in `./task.cfg` and update the corresponding `.cfg` file in the `./deploy` folder, then run `./task deploy` to build and deploy the site.

To host the repository on [Netlify](http://www.netlify.com) set the `target` to `netlify`. In your site settings (Settings > Build & Deploy > Continuous Deployment) update Build Command to `./task deploy` and Publish Directory to `build`.