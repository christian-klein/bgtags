import QRCode from "react-qr-code";
import { Card, CardHeader } from "@mui/material";
import CardActions from "@mui/material/CardActions";
import CardContent from "@mui/material/CardContent";
import Button from "@mui/material/Button";
import Typography from "@mui/material/Typography";

const BgPanel = (props) => {
  var pathToRules = window.location.origin + props.rulesUrl;

  return (
    <Card sx={{ minWidth: 275 }}>
      <CardHeader title={props.title} />
      <CardContent>
        <Typography sx={{ mb: 1.5 }} color="text.secondary">
          {props.minPlayers}-{props.maxPlayers} Players
        </Typography>
        <Typography variant="body2">
          <a href={pathToRules}>
            <img src={props.image} alt={props.title} className="imgCard" />
            <QRCode value={pathToRules} />
          </a>
        </Typography>
      </CardContent>
      <CardActions>
        <Button size="small">BGG</Button>
      </CardActions>
    </Card>
  );
};

export default BgPanel;
