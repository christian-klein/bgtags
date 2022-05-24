import { Grid } from "@mui/material";
import BgPanel from "./components/BgPanel";
import myGames from "./data.json";
import Box from "@mui/material/Box";

function App() {
  return (
    <Box sx={{ flexGrow: 1 }}>
      <Grid container spacing={1}>
        {myGames &&
          myGames.map(({ id, name, url, image, minPlayers, maxPlayers }) => (
           
              <Grid item xs={12} md={4} key={id}>
                <BgPanel
                  title={name}
                  rulesUrl={"/rules/" + url}
                  image={"/img/" + image}
                  minPlayers={minPlayers}
                  maxPlayers={maxPlayers}
                />
              </Grid>
           
          ))}
      </Grid>
    </Box>
  );
}

export default App;
