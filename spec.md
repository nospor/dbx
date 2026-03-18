# Tui database client

## Features
- application should be able to connect to a database (postgres, mysql, sqlite, mssql, possibly others in the future) and execute queries
- application should be able to display query results in a tabular format
- should work in vim mode (doesnt have to implement all vim features but should have normal and insert mode and some basic motions and commands)
- should be able to save and load query history for each connection/database
- should have master key (eg space) to trigger commands
- should show in small popup eg right bottom available commands when triggered
- all/most things should be configurable and saved in ~/.config/dbx/config.json
- history should be saved in ~/.config/dbx/history.json
- should have themes some like light/dark and default (terminal) and be customizable via config file
- for each panel should show help/commands available for that panel when master key is pressed and that panel is focused, eg when explorer is focused and space is pressed then show commands related to explorer, when query editor is focused then show commands related to query editor, etc
- master key + f should show in full window current panel with all its content, eg full window explorer, full window query editor, full window query results. pressing it again should return to normal view with 3 panels. 

## View and panels
one main view split vertically into 2 panes. left one for explored and right one split horizontally into 2 panes. top one for query editor and bottom one for query results.

## explorer pane (letter e to  jump to it)
- should show list of connections and databases
- should allow to select connection and database
- should show list of tables and views for selected database
- should allow to select table/view and show its columns and data types
- pressing `s` on table should write query to select first 100 records from that table and of course execute that query
- should allow to add connection (host, port, user password, database, etc) and save it to config file, when database will be empty then connection should show all databases in explorer. when just one database then showing that one database. database field can contain databases names split via comma, eg `db1,db2,db3` and then those databases will be shown in explorer. if database field is empty then all databases will be shown in explorer.
- should allow to edit connection and save it to config file
- should allow to delete connection and remove it from config file
- should allow to refresh list of tables and views for selected database
- should allow to toggle that view eg space + e to show/hide explorer pane
- width of that pane should be configurable (in percent)

## query editor pane (letter q to jump to it))
- should allow to write and execute queries
- should have syntax highlighting for SQL
- should have autocomplete for table and column names based on selected database
- should automatically save query to history when executed 
- should allow to keep many queries, queries seperated by at least one empty line and execute query on each cursor lies, eg
```
query1

query2

multiline
query3
```
- should work in vim mode, so normal mode and insert mode, in normal mode should be able to navigate between queries and execute them, in insert mode should be able to write queries. should allow for vim motions to move and to edit
- for each connection/database there should be seperate tabs for query editor, so when switching connection/database then query editor should switch to corresponding tab with queries for that connection/database
- history should be remembers for that connection/database and should be loaded when switching to that connection/database
- height of that pane should be configurable (in percent) (default 50%)

## query results pane (letter r to jump to it)
- should display query results in a tabular format
- should allow to scroll through results if they exceed the pane size
- should allow to copy cell value or row to clipboard
- should allow to export results to csv or json
- should show error message if query execution fails
- height of that pane should be configurable (in percent)
