import BgPanel from "./components/BgPanel";
import myGames from './data.json'

function App() {
  
  return (
    <div>
      <h1>My Games</h1>

      {myGames && 
        myGames.map(({id,name,url, image}) => (
        <div key={id}>
          <BgPanel title={name} rulesUrl={'/rules/'+url} image={'/img/'+image} />
        </div>
      ))}
    </div>
  );
}

export default App;
